package server

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// tlsRecordHandshake is the first byte of every TLS handshake record. The bulbs
// observed in the original capture spoke cleartext MQTT (securemode=2), but
// hardware in the field also connects with TLS on the same port, so the server
// sniffs the first byte and handles whichever arrives.
const tlsRecordHandshake = 0x16

// certSource issues self-signed certificates on demand, one per requested
// server name, from a single long-lived key.
//
// A per-name certificate is generated because we cannot know in advance which
// hostname the bulb will ask for, and a name mismatch is the one rejection we
// can rule out cheaply.
//
// The key is deliberately RSA, not the ECDSA a greenfield service would use.
// The bulbs offer only TLS_RSA_* cipher suites, which carry the session key
// encrypted to the server's public key; Go will not select any of them unless
// the certificate itself is RSA. An ECDSA certificate here produces "no cipher
// suite supported by both client and server" no matter what the suite list says.
type certSource struct {
	keyPath string

	mu    sync.Mutex
	key   *rsa.PrivateKey
	certs map[string]*tls.Certificate
}

func newCertSource(keyPath string) *certSource {
	return &certSource{keyPath: keyPath, certs: make(map[string]*tls.Certificate)}
}

// loadKey reads the persisted key, generating and saving one on first run. The
// key is stable across restarts so a bulb that has seen this server before is
// not presented with a brand new identity every time.
func (c *certSource) loadKey() (*rsa.PrivateKey, error) {
	if c.key != nil {
		return c.key, nil
	}
	if key := c.readKey(); key != nil {
		c.key = key
		return key, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generating a TLS key: %w", err)
	}
	if c.keyPath != "" {
		if err := os.MkdirAll(filepath.Dir(c.keyPath), 0o700); err == nil {
			// A key we cannot persist still works for this run; it just means
			// the certificate changes on restart. Not worth refusing to start.
			_ = os.WriteFile(c.keyPath, x509.MarshalPKCS1PrivateKey(key), 0o600)
		}
	}
	c.key = key
	return key, nil
}

// readKey loads the persisted key, returning nil when there is nothing usable
// there. A key that cannot be used is replaced rather than treated as a fatal
// error: it is a self-signed key we generated ourselves, it protects nothing
// that outlives the connection, and a bulb that cannot connect because of a
// stale file on disk is a far worse outcome than a new certificate.
//
// This path matters in practice — earlier builds wrote an ECDSA key here, and
// the RSA key exchange the bulbs require made that key unusable.
func (c *certSource) readKey() *rsa.PrivateKey {
	if c.keyPath == "" {
		return nil
	}
	raw, err := os.ReadFile(c.keyPath)
	if err != nil {
		return nil // first run
	}
	if key, err := x509.ParsePKCS1PrivateKey(raw); err == nil {
		return key
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(raw); err == nil {
		if key, ok := parsed.(*rsa.PrivateKey); ok {
			return key
		}
	}
	slog.Default().Warn("replacing an unusable TLS key",
		"path", c.keyPath,
		"reason", "not an RSA key; the bulbs' RSA key exchange needs one",
		"action", "a new key and certificate are being generated")
	return nil
}

// certificateFor returns a self-signed certificate valid for name.
func (c *certSource) certificateFor(name string) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cert, ok := c.certs[name]; ok {
		return cert, nil
	}
	key, err := c.loadKey()
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating a certificate serial: %w", err)
	}
	subject := name
	if subject == "" {
		subject = "haigosmart"
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: subject, Organization: []string{"haigosmart"}},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if ip := net.ParseIP(name); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else if name != "" {
		template.DNSNames = []string{name}
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("creating a certificate for %q: %w", name, err)
	}
	cert := &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	c.certs[name] = cert
	return cert, nil
}

// bulbCipherSuites are the suites the Aigo firmware offers. All are RSA key
// exchange with CBC, which modern Go disables by default: they have no forward
// secrecy, so a leak of the server key would expose past sessions.
//
// That trade-off does not apply here. The key never leaves the operator's
// machine, the traffic never leaves the LAN, and the alternative is not a
// stronger cipher — it is a bulb that cannot connect and falls back to the
// vendor's cloud, which is the outcome this whole project exists to prevent.
//
// TLS_RSA_WITH_AES_256_CBC_SHA256 (0x003D) is also offered by the bulbs but is
// not implemented by Go at all; the other three are enough.
var bulbCipherSuites = []uint16{
	tls.TLS_RSA_WITH_AES_256_CBC_SHA,    // 0x0035
	tls.TLS_RSA_WITH_AES_128_CBC_SHA256, // 0x003C
	tls.TLS_RSA_WITH_AES_128_CBC_SHA,    // 0x002F
	// Anything newer a future bulb might bring, so this list is not a ceiling.
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
}

// tlsConfig builds the server's TLS configuration. It is deliberately
// permissive: these are embedded devices on a trusted LAN, and refusing an old
// cipher suite would only mean a bulb that will not connect at all.
func (s *Server) tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS10,
		CipherSuites: bulbCipherSuites,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// Record what the bulb offered. If the handshake then fails, this is
			// the evidence that says why: a client offering only PSK cipher
			// suites is using Alibaba's iTLS and cannot be served with a
			// certificate at all.
			s.logClientHello(hello)
			return s.certs.certificateFor(hello.ServerName)
		},
	}
}

// peekedConn re-serves bytes already read from the underlying connection during
// protocol sniffing, so the TLS handshake sees the stream from its first byte.
type peekedConn struct {
	net.Conn
	reader io.Reader
}

func (p *peekedConn) Read(b []byte) (int, error) { return p.reader.Read(b) }

// errNothingSent marks a connection that closed before a single byte arrived.
// Nothing was said, so nothing can be malformed: it is a health probe, a port
// scan, or a load balancer, not a protocol violation. Callers report it quietly.
var errNothingSent = errors.New("connection closed before sending anything")

// sniff looks at the first byte of a connection and, if it is the start of a
// TLS handshake, wraps the connection in a TLS server. A cleartext connection
// is returned untouched.
func (s *Server) sniff(conn net.Conn) (net.Conn, *bufio.Reader, error) {
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, nil, err
	}
	br := bufio.NewReader(conn)
	first, err := br.Peek(1)
	if err != nil {
		// Not a byte arrived, so nothing was said and nothing can be malformed.
		// This is what a Kubernetes tcpSocket probe looks like — connect, close,
		// repeat every few seconds — and equally what a port scanner or a load
		// balancer check looks like. Reporting it as a protocol error fills an
		// idle server's records with the health check that is proving it healthy.
		return nil, nil, fmt.Errorf("%w from %s: %w", errNothingSent, conn.RemoteAddr(), err)
	}
	if first[0] != tlsRecordHandshake {
		return conn, br, nil
	}
	tlsConn := tls.Server(&peekedConn{Conn: conn, reader: br}, s.tlsConfig())
	if err := tlsConn.Handshake(); err != nil {
		return nil, nil, fmt.Errorf("TLS handshake with %s failed: %w", conn.RemoteAddr(), err)
	}
	return tlsConn, bufio.NewReader(tlsConn), nil
}
