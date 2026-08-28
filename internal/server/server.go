package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"haigosmart/internal/events"
	"haigosmart/internal/registry"
)

// DefaultAddr is the listen address. Port 1883 is where the bulbs already send
// their MQTT traffic, so redirecting their cloud hostname is the only change
// the operator makes.
const DefaultAddr = ":1883"

// Server accepts bulb connections, in cleartext or over TLS.
type Server struct {
	reg   *registry.Registry
	bus   *events.Bus
	certs *certSource

	// Admit decides whether a bulb may be served, once it has identified
	// itself. Nil admits everything, which is what the interactive mode wants
	// and what every earlier release did — so today's behaviour is preserved by
	// construction rather than by a branch someone has to remember.
	//
	// It is a predicate rather than a set so that this package stays unaware of
	// where the configuration comes from. Set it before Serve.
	Admit func(deviceID string) bool

	// now is swappable so tests need not sleep.
	nowFn func() time.Time

	// takeovers records when each device id last displaced its own connection,
	// so a genuine duplicate can be told apart from an ordinary reconnect.
	takeoverMu sync.Mutex
	takeovers  map[string][]time.Time

	// rejections records, per device id, when a refusal was last reported and
	// how many have been suppressed since.
	rejectMu sync.Mutex
	rejects  map[string]*rejection

	wg sync.WaitGroup
}

// New returns a server writing into reg and publishing to bus. keyPath is where
// the self-signed TLS key is kept; an empty path keeps it in memory only, which
// means a new certificate on every restart.
func New(reg *registry.Registry, bus *events.Bus, keyPath string) *Server {
	return &Server{
		reg: reg, bus: bus, certs: newCertSource(keyPath),
		nowFn: time.Now, takeovers: make(map[string][]time.Time),
		rejects: make(map[string]*rejection),
	}
}

func (s *Server) now() time.Time { return s.nowFn() }

func (s *Server) publish(e events.Event) { s.bus.Publish(e) }

// Serve accepts connections until ctx is cancelled or the listener fails.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.wg.Wait()
			if ctx.Err() != nil {
				return nil // a clean shutdown, not a failure
			}
			return fmt.Errorf("accepting connections: %w", err)
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			// The bulbs in the original capture spoke cleartext MQTT, but field
			// hardware also arrives with TLS on the same port. Sniff rather than
			// assume, so one listener serves both.
			ready, reader, err := s.sniff(conn)
			if err != nil {
				conn.Close()
				s.publish(events.Event{
					At: s.now(), Kind: events.ProtocolError,
					Detail: err.Error(),
				})
				return
			}
			sess := &session{
				conn:       ready,
				reader:     reader,
				srv:        s,
				pending:    make(map[string]*pendingCommand),
				pendingSeq: make(map[string]*pendingCommand),
			}
			sess.serve(ctx)
		}()
	}
}

// ListenAndServe binds addr and serves until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w. is another process using the port, or does binding it need privileges?", addr, err)
	}
	return s.Serve(ctx, ln)
}

// takeoverWindow and takeoverThreshold decide when repeated connection
// takeovers stop looking like reconnects and start looking like two devices
// fighting over one identity.
const (
	takeoverWindow    = time.Minute
	takeoverThreshold = 3
)

// noteTakeover records that a device id displaced its own connection, and
// reports whether it has now happened often enough to be worth warning about.
//
// One takeover is routine: a bulb power-cycles and reconnects before the server
// notices the old socket is dead. Several in a minute are not — that is two
// devices presenting the same id and kicking each other off in a loop, which is
// the case the duplicate warning actually exists for.
func (s *Server) noteTakeover(deviceID string, now time.Time) bool {
	s.takeoverMu.Lock()
	defer s.takeoverMu.Unlock()

	kept := s.takeovers[deviceID][:0]
	for _, at := range s.takeovers[deviceID] {
		if now.Sub(at) < takeoverWindow {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now)
	s.takeovers[deviceID] = kept
	return len(kept) >= takeoverThreshold
}

// rejectionWindow is how often one device id's refusal is worth reporting. A
// refused bulb reconnects indefinitely, so without this the record stream would
// be unbounded and the interesting records would be buried in it.
const rejectionWindow = 5 * time.Minute

type rejection struct {
	reportedAt time.Time
	firstAt    time.Time
	suppressed int
}

// noteRejection reports whether this refusal is worth a record, and how many
// went unreported since the last one. The count is carried in the record so the
// suppression is visible rather than silent.
func (s *Server) noteRejection(deviceID string, now time.Time) (report bool, suppressed int, since time.Time) {
	s.rejectMu.Lock()
	defer s.rejectMu.Unlock()

	r, seen := s.rejects[deviceID]
	if !seen {
		s.rejects[deviceID] = &rejection{reportedAt: now}
		return true, 0, time.Time{}
	}
	if now.Sub(r.reportedAt) < rejectionWindow {
		if r.suppressed == 0 {
			r.firstAt = now
		}
		r.suppressed++
		return false, 0, time.Time{}
	}
	suppressed, since = r.suppressed, r.firstAt
	r.reportedAt, r.suppressed, r.firstAt = now, 0, time.Time{}
	return true, suppressed, since
}

// logClientHello records what a TLS client offered. When a handshake fails this
// is the evidence that explains it — in particular, a client offering only
// PSK cipher suites is using a pre-shared key we do not have, and no
// certificate we generate will ever satisfy it.
func (s *Server) logClientHello(hello *tls.ClientHelloInfo) {
	names := make([]string, 0, len(hello.CipherSuites))
	psk := false
	for _, id := range hello.CipherSuites {
		name := tls.CipherSuiteName(id)
		names = append(names, name)
		if strings.Contains(name, "PSK") || (name == fmt.Sprintf("0x%04X", id) && isPSKSuite(id)) {
			psk = true
		}
	}
	attrs := []any{
		"remote", hello.Conn.RemoteAddr().String(),
		"sni", hello.ServerName,
		"versions", hello.SupportedVersions,
		"cipher_suites", names,
	}
	if psk {
		attrs = append(attrs, "psk_only_client", true)
	}
	// This is diagnostic detail, not an event: it happens on every connect and
	// every reconnect, and putting it in the operator's feed would drown the
	// things they actually need to see. A failed handshake is reported
	// separately, by the caller, and that one is an event.
	slog.Default().Debug("TLS client hello from a bulb", attrs...)
	if psk {
		s.publish(events.Event{
			At: s.now(), Kind: events.ProtocolError,
			Detail: fmt.Sprintf("%s uses TLS-PSK (Alibaba iTLS): no certificate can satisfy it, "+
				"the device secret would have to be extracted from the firmware", hello.Conn.RemoteAddr()),
		})
	}
}

// isPSKSuite reports whether a cipher suite id is one of the TLS-PSK families.
// Go's crypto/tls implements none of them, so a client that offers nothing else
// cannot be served at all — worth naming precisely rather than reporting a bare
// handshake failure.
func isPSKSuite(id uint16) bool {
	switch {
	case id >= 0x008A && id <= 0x008D, // TLS_PSK_WITH_*
		id >= 0x00A8 && id <= 0x00AF, // TLS_PSK_WITH_AES_*_GCM / SHA256
		id >= 0xC0A4 && id <= 0xC0B5, // TLS_PSK_WITH_AES_*_CCM
		id >= 0xC033 && id <= 0xC03B, // TLS_ECDHE_PSK_WITH_*
		id == 0xCCAB || id == 0xCCAC, // CHACHA20_POLY1305_SHA256 PSK
		id >= 0xD001 && id <= 0xD005: // TLS_ECDHE_PSK_WITH_AES_*_CCM_SHA256
		return true
	}
	return false
}

// commandID pulls the id back out of an encoded command so the acknowledgement
// can be correlated to it.
func commandID(payload []byte) (string, error) {
	var msg struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return "", fmt.Errorf("reading back command id: %w", err)
	}
	return msg.ID, nil
}
