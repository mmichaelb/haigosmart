// Package fakebulb speaks the bulb half of the protocol in-process, so the
// server, registry, control and TUI layers are testable without hardware.
//
// It is written from the captured fixtures rather than from our idea of how the
// bulbs behave, so a test that passes against it is evidence about the real
// protocol and not merely agreement with our own assumptions.
package fakebulb

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"haigosmart/internal/protocol"
)

// Options configure a fake bulb.
type Options struct {
	ProductKey string
	DeviceName string
	// Version is the firmware string; it decides the reported capabilities.
	// Empty means the bulb never sends an OTA inform at all.
	Version string
	// KeepAlive is what the bulb declares in CONNECT.
	KeepAlive uint16
	// Malformed makes the bulb send a garbage packet right after connecting.
	Malformed bool
	// Silent makes the bulb accept commands but never acknowledge them, which
	// is how a real bulb behaves when it is wedged.
	Silent bool
	// TLS makes the bulb wrap its connection in TLS, as field hardware does,
	// without verifying the server's certificate.
	TLS bool
	// NoAck makes the bulb skip the CommonService_reply but still report the
	// resulting state, echoing the command's seq. Real firmware is not
	// guaranteed to send both.
	NoAck bool
	// LegacyTLS restricts the handshake to what the real Aigo firmware offers:
	// TLS 1.2 with RSA key exchange and CBC, nothing else. Modern Go disables
	// these by default, so this is the configuration that actually catches a
	// regression the permissive default would sail past.
	LegacyTLS bool
}

// Bulb is a fake bulb driving one connection.
type Bulb struct {
	opts Options
	conn net.Conn
	w    *bufio.Writer
	r    *bufio.Reader

	mu      sync.Mutex
	power   bool
	bright  uint8
	temp    uint8
	postID  int
	stopped bool

	commands chan map[string]any
}

// Dial connects a fake bulb to addr and completes the handshake.
func Dial(addr string, opts Options) (*Bulb, error) {
	if opts.ProductKey == "" {
		opts.ProductKey = "a1GGnyln558"
	}
	if opts.DeviceName == "" {
		opts.DeviceName = "703e975dc388"
	}
	if opts.KeepAlive == 0 {
		opts.KeepAlive = 120
	}
	var conn net.Conn
	var err error
	if opts.TLS || opts.LegacyTLS {
		// Real bulbs accept whatever certificate the server presents; that is
		// what makes a replacement server possible at all.
		cfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // impersonating a device that does not verify
		if opts.LegacyTLS {
			cfg.MinVersion = tls.VersionTLS12
			cfg.MaxVersion = tls.VersionTLS12
			// Exactly what the captured firmware offers, minus the one suite Go
			// does not implement (TLS_RSA_WITH_AES_256_CBC_SHA256).
			cfg.CipherSuites = []uint16{
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
				tls.TLS_RSA_WITH_AES_128_CBC_SHA,
			}
		}
		conn, err = tls.Dial("tcp", addr, cfg)
	} else {
		conn, err = net.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("fakebulb: dialling %s: %w", addr, err)
	}
	b := &Bulb{
		opts: opts, conn: conn,
		w: bufio.NewWriter(conn), r: bufio.NewReader(conn),
		bright: 30, temp: 2, power: true,
		commands: make(chan map[string]any, 32),
	}
	if err := b.handshake(); err != nil {
		conn.Close()
		return nil, err
	}
	go b.readLoop()
	return b, nil
}

// DeviceID is the identifier the server will register this bulb under.
func (b *Bulb) DeviceID() string { return b.opts.DeviceName }

// Close hangs up, the way a power cut does.
func (b *Bulb) Close() error {
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
	return b.conn.Close()
}

// Commands yields the property maps the server has sent to this bulb.
func (b *Bulb) Commands() <-chan map[string]any { return b.commands }

func (b *Bulb) topic(suffix string) string {
	return fmt.Sprintf("/sys/%s/%s/%s", b.opts.ProductKey, b.opts.DeviceName, suffix)
}

func (b *Bulb) send(p []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return net.ErrClosed
	}
	if _, err := b.w.Write(p); err != nil {
		return err
	}
	return b.w.Flush()
}

func (b *Bulb) handshake() error {
	if err := b.send(b.encodeConnect()); err != nil {
		return fmt.Errorf("fakebulb: sending CONNECT: %w", err)
	}
	pkt, err := protocol.ReadPacket(b.r)
	if err != nil {
		return fmt.Errorf("fakebulb: reading CONNACK: %w", err)
	}
	if pkt.Type != protocol.TypeConnack {
		return fmt.Errorf("fakebulb: expected CONNACK, got type %d", pkt.Type)
	}
	if b.opts.Malformed {
		// A remaining length that never terminates: the server must drop this
		// connection and keep serving everyone else.
		return b.send([]byte{0x30, 0xff, 0xff, 0xff, 0xff, 0xff})
	}
	if err := b.subscribe(); err != nil {
		return err
	}
	if b.opts.Version != "" {
		if err := b.informVersion(); err != nil {
			return err
		}
	}
	return b.PostFullState()
}

func (b *Bulb) encodeConnect() []byte {
	clientID := fmt.Sprintf("%s.%s|securemode=2,tokenType=0,_v=sdk-c-2.3.0,authtype=custom-ilop|",
		b.opts.ProductKey, b.opts.DeviceName)
	username := b.opts.DeviceName + "&" + b.opts.ProductKey

	body := []byte{0, 4, 'M', 'Q', 'T', 'T', 4, 0xc0}
	body = binary.BigEndian.AppendUint16(body, b.opts.KeepAlive)
	for _, s := range []string{clientID, username, "fakebulbpassword"} {
		body = binary.BigEndian.AppendUint16(body, uint16(len(s)))
		body = append(body, s...)
	}
	return protocol.Encode(protocol.TypeConnect, 0, body)
}

func (b *Bulb) subscribe() error {
	topic := b.topic("thing/event/+/post_reply")
	body := binary.BigEndian.AppendUint16(nil, 1)
	body = binary.BigEndian.AppendUint16(body, uint16(len(topic)))
	body = append(body, topic...)
	body = append(body, 0)
	return b.send(protocol.Encode(protocol.TypeSubscribe, 2, body))
}

func (b *Bulb) informVersion() error {
	payload, _ := json.Marshal(map[string]any{
		"id": "2", "params": map[string]any{"version": b.opts.Version},
	})
	topic := fmt.Sprintf("/ota/device/inform/%s/%s", b.opts.ProductKey, b.opts.DeviceName)
	return b.send(protocol.EncodePublish(topic, payload))
}

// PostFullState sends the full property report a bulb makes on power-up, with
// bare scalar values exactly as the capture shows.
func (b *Bulb) PostFullState() error {
	b.mu.Lock()
	power, bright, temp := b.power, b.bright, b.temp
	b.postID++
	id := fmt.Sprint(b.postID)
	b.mu.Unlock()

	switchValue := 0
	if power {
		switchValue = 1
	}
	payload, _ := json.Marshal(map[string]any{
		"id": id, "version": "1.0", "method": "thing.event.property.post",
		"params": map[string]any{
			"LightType": 1, "LightSwitch": switchValue, "WorkMode": 0, "LightMode": 0,
			"ColorTemperature": temp, "Brightness": bright,
		},
	})
	return b.send(protocol.EncodePublish(b.topic("thing/event/property/post"), payload))
}

// PostChange reports a state change the way a wall switch or a completed
// command does: a delta with {"value":…,"time":…} wrappers.
func (b *Bulb) PostChange(props map[string]any, seq string) error {
	b.mu.Lock()
	b.postID++
	id := fmt.Sprint(b.postID)
	b.applyLocked(props)
	b.mu.Unlock()

	now := time.Now().UnixMilli()
	params := make(map[string]any, len(props)+1)
	for name, value := range props {
		params[name] = map[string]any{"value": value, "time": now}
	}
	if seq != "" {
		params["CommonServiceResponse"] = map[string]any{
			"value": map[string]any{"seq": seq}, "time": now,
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"id": id, "version": "1.0", "method": "thing.event.property.post", "params": params,
	})
	return b.send(protocol.EncodePublish(b.topic("thing/event/property/post"), payload))
}

// SetPower simulates someone using the wall switch.
func (b *Bulb) SetPower(on bool) error {
	value := 0
	if on {
		value = 1
	}
	return b.PostChange(map[string]any{"LightSwitch": value}, "")
}

func (b *Bulb) applyLocked(props map[string]any) {
	for name, value := range props {
		n, ok := toFloat(value)
		if !ok {
			continue
		}
		switch name {
		case "LightSwitch":
			b.power = n != 0
		case "Brightness":
			b.bright = uint8(n)
		case "ColorTemperature":
			b.temp = uint8(n)
		}
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case uint8:
		return float64(n), true
	}
	return 0, false
}

// Ping sends a keep-alive.
func (b *Bulb) Ping() error {
	return b.send(protocol.Encode(protocol.TypePingreq, 0, nil))
}

// readLoop handles commands from the server: acknowledge, then report the new
// state, which is what the real bulb does.
func (b *Bulb) readLoop() {
	for {
		pkt, err := protocol.ReadPacket(b.r)
		if err != nil {
			close(b.commands)
			return
		}
		if pkt.Type != protocol.TypePublish {
			continue
		}
		pub, err := protocol.DecodePublish(pkt.Payload, pkt.QoS())
		if err != nil || !strings.HasSuffix(pub.Topic, "/thing/service/CommonService") {
			continue
		}
		id, props, seq := decodeCommand(pub.Payload)
		select {
		case b.commands <- props:
		default:
		}
		if b.opts.Silent {
			continue
		}
		// Real hardware acts on single-property commands only. A bundle is
		// ignored outright — no acknowledgement, no state report — which is
		// exactly how a bundled command times out on a real bulb.
		if len(props) != 1 {
			continue
		}
		if !b.opts.NoAck {
			reply, _ := json.Marshal(map[string]any{"id": id, "code": 200, "data": map[string]any{}})
			if err := b.send(protocol.EncodePublish(b.topic("thing/service/CommonService_reply"), reply)); err != nil {
				return
			}
		}
		if err := b.PostChange(props, seq); err != nil {
			return
		}
	}
}

func decodeCommand(payload []byte) (id string, props map[string]any, seq string) {
	var msg struct {
		ID     string `json:"id"`
		Params struct {
			Params string `json:"params"`
			Seq    string `json:"seq"`
		} `json:"params"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return "", nil, ""
	}
	props = map[string]any{}
	_ = json.Unmarshal([]byte(msg.Params.Params), &props)
	return msg.ID, props, msg.Params.Seq
}
