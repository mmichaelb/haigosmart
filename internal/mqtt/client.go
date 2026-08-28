// Package mqtt is a minimal MQTT 3.1.1 client for publishing to the household's
// broker.
//
// It is written here rather than imported because most of it already existed:
// internal/protocol carries a codec tested against real captured bytes, built to
// serve the bulbs. What is new is keep-alive, reconnect, and last-will handling.
// See specs/002-homeassistant-integration/research.md section 2 for the full
// reasoning, including the named fallback if this proves to be the wrong call.
//
// Scope is deliberately narrow: one publisher, a handful of topics, QoS 0 and 1.
// QoS 2 and persistent sessions are not implemented and are not needed.
package mqtt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mmichaelb/haigosmart/internal/protocol"
)

// Options configure a client.
type Options struct {
	Broker    string // host:port
	ClientID  string
	Username  string
	Password  string
	KeepAlive time.Duration

	// Will is published by the broker if the connection dies without a
	// DISCONNECT. It is the only mechanism that reports a crash or a pulled
	// cable, which a shutdown handler cannot.
	WillTopic   string
	WillPayload []byte
	WillRetain  bool

	// OnConnect runs after every successful connect, including reconnects, so
	// the caller can republish retained state.
	OnConnect func()

	Logger *slog.Logger
}

// Publisher is what consumers depend on, so a different client implementation
// can be substituted without touching them.
type Publisher interface {
	Publish(topic string, payload []byte, retain bool) error
	Subscribe(topic string, handler func(topic string, payload []byte)) error
}

// Client is a connection to a broker that re-establishes itself.
type Client struct {
	opts Options
	log  *slog.Logger

	mu       sync.Mutex
	conn     net.Conn
	handlers map[string]func(topic string, payload []byte)
	packetID uint16

	connected atomic.Bool
	done      chan struct{}
	closeOnce sync.Once
}

// ErrNotConnected means the client has no live connection right now. It is not
// fatal: the caller's message is dropped and the client keeps reconnecting.
var ErrNotConnected = errors.New("mqtt: not connected to the broker")

// New returns a client. Nothing happens until Run is called.
func New(opts Options) *Client {
	if opts.ClientID == "" {
		opts.ClientID = "haigosmart"
	}
	if opts.KeepAlive <= 0 {
		opts.KeepAlive = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Client{
		opts:     opts,
		log:      opts.Logger,
		handlers: make(map[string]func(string, []byte)),
		done:     make(chan struct{}),
	}
}

// Connected reports whether a connection is currently established.
func (c *Client) Connected() bool { return c.connected.Load() }

// Run connects and keeps reconnecting until ctx is cancelled. It returns only
// when ctx ends, because a broker being down is a temporary condition and not a
// reason to give up: the lamps keep working from the terminal regardless.
func (c *Client) Run(ctx context.Context) error {
	defer c.closeOnce.Do(func() { close(c.done) })

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if err := c.session(ctx); err != nil && ctx.Err() == nil {
			c.log.Warn("mqtt connection lost", "broker", c.opts.Broker, "error", err,
				"retry_in", backoff.String())
		}
		if ctx.Err() != nil {
			return nil
		}

		// Jitter so a broker restarting does not meet every client at once.
		wait := backoff + time.Duration(rand.Int64N(int64(backoff/2)+1))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// session runs one connection from CONNECT to disconnection.
func (c *Client) session(ctx context.Context) error {
	conn, err := net.DialTimeout("tcp", c.opts.Broker, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dialling %s: %w", c.opts.Broker, err)
	}
	defer conn.Close()

	r := bufio.NewReader(conn)
	if err := c.handshake(conn, r); err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	handlers := make(map[string]func(string, []byte), len(c.handlers))
	for topic, h := range c.handlers {
		handlers[topic] = h
	}
	c.mu.Unlock()
	c.connected.Store(true)
	defer func() {
		c.connected.Store(false)
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()

	// Re-establish subscriptions from the previous life of this client.
	for topic := range handlers {
		if err := c.sendSubscribe(topic); err != nil {
			return err
		}
	}
	c.log.Info("connected to the mqtt broker", "broker", c.opts.Broker)
	if c.opts.OnConnect != nil {
		go c.opts.OnConnect()
	}

	stop := context.AfterFunc(ctx, func() {
		// A deliberate departure: tell the broker, so it discards the will.
		_ = c.writeRaw(protocol.EncodeDisconnect())
		conn.Close()
	})
	defer stop()

	go c.keepAlive(ctx, conn)
	return c.readLoop(r)
}

func (c *Client) handshake(conn net.Conn, r *bufio.Reader) error {
	packet := protocol.EncodeConnect(protocol.ConnectOptions{
		ClientID:     c.opts.ClientID,
		KeepAlive:    uint16(c.opts.KeepAlive.Seconds()),
		Username:     c.opts.Username,
		Password:     c.opts.Password,
		WillTopic:    c.opts.WillTopic,
		WillPayload:  c.opts.WillPayload,
		WillRetain:   c.opts.WillRetain,
		CleanSession: true,
	})
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("sending CONNECT: %w", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	pkt, err := protocol.ReadPacket(r)
	if err != nil {
		return fmt.Errorf("reading CONNACK: %w", err)
	}
	if pkt.Type != protocol.TypeConnack {
		return fmt.Errorf("expected CONNACK, got packet type %d", pkt.Type)
	}
	_, code, err := protocol.DecodeConnack(pkt.Payload)
	if err != nil {
		return err
	}
	if code != protocol.ConnackAcceptedCode {
		// A rejection is not a network problem and must not read like one.
		return fmt.Errorf("broker %s refused the connection: %s", c.opts.Broker, protocol.ConnackReason(code))
	}
	return conn.SetReadDeadline(time.Time{})
}

func (c *Client) readLoop(r *bufio.Reader) error {
	for {
		pkt, err := protocol.ReadPacket(r)
		if err != nil {
			return err
		}
		if pkt.Type != protocol.TypePublish {
			continue
		}
		pub, err := protocol.DecodePublish(pkt.Payload, pkt.QoS())
		if err != nil {
			// A malformed message from the broker is logged and skipped. It must
			// not drop the connection and take every lamp's entity with it.
			c.log.Warn("mqtt: could not decode an inbound message", "error", err)
			continue
		}
		if pkt.QoS() == 1 {
			_ = c.writeRaw(protocol.EncodePuback(pub.PacketID))
		}
		c.dispatch(pub.Topic, pub.Payload)
	}
}

func (c *Client) dispatch(topic string, payload []byte) {
	c.mu.Lock()
	var handler func(string, []byte)
	for filter, h := range c.handlers {
		if topicMatches(filter, topic) {
			handler = h
			break
		}
	}
	c.mu.Unlock()
	if handler != nil {
		handler(topic, payload)
	}
}

func (c *Client) keepAlive(ctx context.Context, conn net.Conn) {
	ticker := time.NewTicker(c.opts.KeepAlive / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			live := c.conn == conn
			c.mu.Unlock()
			if !live {
				return
			}
			if err := c.writeRaw(protocol.Encode(protocol.TypePingreq, 0, nil)); err != nil {
				return
			}
		}
	}
}

// Publish sends a message. A message published while disconnected is dropped
// with ErrNotConnected rather than queued: the retained state is republished on
// reconnect anyway, so queueing would only deliver stale values late.
func (c *Client) Publish(topic string, payload []byte, retain bool) error {
	var packet []byte
	if retain {
		packet = protocol.EncodePublishRetained(topic, payload)
	} else {
		packet = protocol.EncodePublish(topic, payload)
	}
	return c.writeRaw(packet)
}

// PublishQoS1 sends a message the broker must acknowledge.
func (c *Client) PublishQoS1(topic string, payload []byte, retain bool) error {
	c.mu.Lock()
	c.packetID++
	if c.packetID == 0 {
		c.packetID = 1
	}
	id := c.packetID
	c.mu.Unlock()
	return c.writeRaw(protocol.EncodePublishQoS1(topic, payload, id, retain))
}

// Subscribe registers a handler for a topic filter. The subscription is
// re-established automatically after a reconnect.
func (c *Client) Subscribe(topic string, handler func(topic string, payload []byte)) error {
	c.mu.Lock()
	c.handlers[topic] = handler
	connected := c.conn != nil
	c.mu.Unlock()
	if !connected {
		return nil // established when the connection comes up
	}
	return c.sendSubscribe(topic)
}

func (c *Client) sendSubscribe(topic string) error {
	c.mu.Lock()
	c.packetID++
	if c.packetID == 0 {
		c.packetID = 1
	}
	id := c.packetID
	c.mu.Unlock()
	return c.writeRaw(protocol.EncodeSubscribe(id, 0, topic))
}

func (c *Client) writeRaw(packet []byte) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return ErrNotConnected
	}
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	_, err := conn.Write(packet)
	return err
}

// topicMatches implements MQTT topic filter matching, including + and #.
func topicMatches(filter, topic string) bool {
	f, t := splitTopic(filter), splitTopic(topic)
	for i, part := range f {
		if part == "#" {
			return true
		}
		if i >= len(t) {
			return false
		}
		if part != "+" && part != t[i] {
			return false
		}
	}
	return len(f) == len(t)
}

func splitTopic(s string) []string {
	var out []string
	start := 0
	for i := range len(s) {
		if s[i] == '/' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
