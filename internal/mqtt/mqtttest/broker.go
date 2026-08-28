// Package mqtttest provides an in-process MQTT broker for tests.
//
// It implements the wire protocol and nothing else. In particular it knows
// nothing about Home Assistant's discovery semantics, so a test asserting on a
// discovery payload is checking that payload against Home Assistant's published
// format rather than against a helper that could be wrong in the same direction
// as the code under test. That distinction cost four bugs in feature 001, where
// a device double built from assumptions agreed with those assumptions.
package mqtttest

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"haigosmart/internal/protocol"
)

// Message is one published message as the broker saw it.
type Message struct {
	Topic    string
	Payload  []byte
	Retained bool
}

// Broker is a minimal MQTT 3.1.1 broker: connect, subscribe, publish, retain,
// keep-alive, and last will.
type Broker struct {
	ln net.Listener

	mu          sync.Mutex
	messages    []Message
	retained    map[string]Message
	subscribers []*session
	connects    int
	credentials [][2]string
	refuseCode  byte
	closed      bool
}

type session struct {
	conn   net.Conn
	topics []string
	will   *Message
	broker *Broker
	mu     sync.Mutex
}

// Start listens on a random loopback port and serves until Close.
func Start() (*Broker, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := &Broker{ln: ln, retained: make(map[string]Message)}
	go b.accept()
	return b, nil
}

// Addr is the broker's address, for a client to dial.
func (b *Broker) Addr() string { return b.ln.Addr().String() }

// Close stops the broker and drops every connection.
func (b *Broker) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	sessions := b.subscribers
	b.subscribers = nil
	b.mu.Unlock()

	b.ln.Close()
	for _, s := range sessions {
		s.conn.Close()
	}
}

// RefuseWith makes the next connection attempt fail with a CONNACK return code,
// so a client's handling of a rejection can be tested.
func (b *Broker) RefuseWith(code byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refuseCode = code
}

// DropConnections closes every live connection without a DISCONNECT, which is
// what a broker restart or a network failure looks like — and which publishes
// any registered wills.
func (b *Broker) DropConnections() {
	b.mu.Lock()
	sessions := append([]*session(nil), b.subscribers...)
	b.subscribers = nil
	b.mu.Unlock()
	for _, s := range sessions {
		s.publishWill()
		s.conn.Close()
	}
}

// Messages returns every message published so far.
func (b *Broker) Messages() []Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Message(nil), b.messages...)
}

// Retained returns the retained message on a topic, if any.
func (b *Broker) Retained(topic string) (Message, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.retained[topic]
	return m, ok
}

// Connects counts accepted CONNECT packets, so a test can watch a reconnect.
func (b *Broker) Connects() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connects
}

// Credentials returns the username and password of each connection.
func (b *Broker) Credentials() [][2]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][2]string(nil), b.credentials...)
}

// WaitFor blocks until cond holds or the timeout passes.
func (b *Broker) WaitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// Publish injects a message from the broker side, as another client would.
func (b *Broker) Publish(topic string, payload []byte) {
	b.deliver(Message{Topic: topic, Payload: payload})
}

func (b *Broker) accept() {
	for {
		conn, err := b.ln.Accept()
		if err != nil {
			return
		}
		go b.serve(conn)
	}
}

func (b *Broker) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	s := &session{conn: conn, broker: b}

	pkt, err := protocol.ReadPacket(r)
	if err != nil || pkt.Type != protocol.TypeConnect {
		return
	}
	c, err := protocol.DecodeConnect(pkt.Payload)
	if err != nil {
		return
	}

	b.mu.Lock()
	code := b.refuseCode
	b.refuseCode = 0
	if code == 0 {
		b.connects++
		b.credentials = append(b.credentials, [2]string{c.Username, c.Password})
		b.subscribers = append(b.subscribers, s)
	}
	b.mu.Unlock()

	if code != 0 {
		_, _ = conn.Write([]byte{protocol.TypeConnack << 4, 2, 0, code})
		return
	}
	if _, err := conn.Write(protocol.ConnackAccepted); err != nil {
		return
	}
	s.captureWill(pkt.Payload)

	for {
		pkt, err := protocol.ReadPacket(r)
		if err != nil {
			s.publishWill()
			b.remove(s)
			return
		}
		if err := s.handle(pkt); err != nil {
			b.remove(s)
			return
		}
	}
}

// captureWill re-parses the CONNECT for its will, which DecodeConnect skips.
func (s *session) captureWill(payload []byte) {
	// Layout: protocol name (2+4) | level (1) | flags (1) | keep-alive (2) | client id...
	// so flags sit at index 7 and the client id starts at 10.
	if len(payload) < 10 {
		return
	}
	flags := payload[7]
	if flags&0x04 == 0 {
		return
	}
	i := 10 // protocol name (6) + level (1) + flags (1) + keep-alive (2)
	readStr := func() (string, bool) {
		if i+2 > len(payload) {
			return "", false
		}
		n := int(payload[i])<<8 | int(payload[i+1])
		if i+2+n > len(payload) {
			return "", false
		}
		v := string(payload[i+2 : i+2+n])
		i += 2 + n
		return v, true
	}
	if _, ok := readStr(); !ok { // client id
		return
	}
	topic, ok := readStr()
	if !ok {
		return
	}
	body, ok := readStr()
	if !ok {
		return
	}
	s.mu.Lock()
	s.will = &Message{Topic: topic, Payload: []byte(body), Retained: flags&0x20 != 0}
	s.mu.Unlock()
}

func (s *session) publishWill() {
	s.mu.Lock()
	will := s.will
	s.will = nil
	s.mu.Unlock()
	if will != nil {
		s.broker.deliver(*will)
	}
}

func (s *session) handle(pkt protocol.Packet) error {
	switch pkt.Type {
	case protocol.TypePingreq:
		_, err := s.conn.Write(protocol.Pingresp)
		return err

	case protocol.TypeSubscribe:
		id, topics, err := protocol.DecodeSubscribe(pkt.Payload)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.topics = append(s.topics, topics...)
		s.mu.Unlock()
		if _, err := s.conn.Write(protocol.EncodeSuback(id, len(topics))); err != nil {
			return err
		}
		// Replay retained messages, which is what makes a subscriber that
		// connects late still see current values.
		for _, topic := range topics {
			for _, m := range s.broker.retainedMatching(topic) {
				if err := s.send(m); err != nil {
					return err
				}
			}
		}
		return nil

	case protocol.TypePublish:
		pub, err := protocol.DecodePublish(pkt.Payload, pkt.QoS())
		if err != nil {
			return err
		}
		if pkt.QoS() == 1 {
			if _, err := s.conn.Write(protocol.EncodePuback(pub.PacketID)); err != nil {
				return err
			}
		}
		s.broker.deliver(Message{Topic: pub.Topic, Payload: pub.Payload, Retained: pkt.Retained()})
		return nil

	case protocol.TypeDisconnect:
		// A deliberate departure: the will is discarded, not published.
		s.mu.Lock()
		s.will = nil
		s.mu.Unlock()
		return fmt.Errorf("client disconnected")

	default:
		return nil
	}
}

func (s *session) send(m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.conn.Write(protocol.EncodePublish(m.Topic, m.Payload))
	return err
}

func (b *Broker) deliver(m Message) {
	b.mu.Lock()
	b.messages = append(b.messages, m)
	if m.Retained {
		if len(m.Payload) == 0 {
			delete(b.retained, m.Topic) // an empty retained payload clears it
		} else {
			b.retained[m.Topic] = m
		}
	}
	targets := append([]*session(nil), b.subscribers...)
	b.mu.Unlock()

	for _, s := range targets {
		s.mu.Lock()
		topics := append([]string(nil), s.topics...)
		s.mu.Unlock()
		for _, filter := range topics {
			if TopicMatches(filter, m.Topic) {
				_ = s.send(m)
				break
			}
		}
	}
}

func (b *Broker) retainedMatching(filter string) []Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Message
	for topic, m := range b.retained {
		if TopicMatches(filter, topic) {
			out = append(out, m)
		}
	}
	return out
}

func (b *Broker) remove(target *session) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subscribers {
		if s == target {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			return
		}
	}
}

// TopicMatches implements MQTT topic filter matching, including + and #.
func TopicMatches(filter, topic string) bool {
	f := strings.Split(filter, "/")
	t := strings.Split(topic, "/")
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
