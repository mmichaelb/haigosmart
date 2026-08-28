// Package server accepts connections from bulbs, speaks their protocol, and
// keeps the registry in step with what they report.
package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"haigosmart/internal/bulb"
	"haigosmart/internal/events"
	"haigosmart/internal/protocol"
)

// pendingCommand is one in-flight command awaiting confirmation from the bulb.
//
// The bulb confirms twice: a CommonService_reply carrying the command's id, and
// a property post echoing the command's seq in CommonServiceResponse. Either is
// proof the command landed, and waiting on both means a bulb that skips one
// still completes the command instead of timing out.
type pendingCommand struct {
	id   string
	seq  string
	done chan error
}

// session is one bulb's connection. It is also that bulb's bulb.Driver.
type session struct {
	conn     net.Conn
	reader   *bufio.Reader
	identity protocol.Identity
	topics   protocol.Topics
	srv      *Server

	writeMu sync.Mutex

	mu         sync.Mutex
	pending    map[string]*pendingCommand // keyed by command id, matched by the ack
	pendingSeq map[string]*pendingCommand // keyed by seq, matched by the state report
	closed     bool
	keepAliv   time.Duration
	// reported records whether this connection has produced a state report yet.
	// The first one is always published, even when it changes nothing: it is
	// the bulb confirming what it is, which is different information from a
	// value the registry remembered across a restart.
	reported bool
}

// DeviceID implements bulb.Driver.
func (s *session) DeviceID() string { return s.identity.DeviceID() }

// Close implements bulb.Driver.
func (s *session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	return s.conn.Close()
}

// Apply implements bulb.Driver: it moves the bulb to the wanted state and
// returns once the bulb has confirmed, or once ctx expires.
//
// Each changed property is sent as its own command, because that is what the
// vendor's app does and what the hardware accepts; see protocol.ChangedProps.
func (s *session) Apply(ctx context.Context, want bulb.LightState) error {
	b, ok := s.srv.reg.View(s.DeviceID())
	if !ok {
		return fmt.Errorf("bulb %s is no longer registered", s.DeviceID())
	}
	props := protocol.ChangedProps(b.State, want, b.Capabilities)
	if len(props) == 0 {
		return nil // already in the wanted state
	}
	// A property that has not been confirmed does not stop the ones after it:
	// the command was delivered, and the bulb confirms in its own time.
	var unconfirmed []string
	for _, prop := range props {
		switch err := s.applyOne(ctx, b.Name, prop); {
		case err == nil:
		case errors.Is(err, bulb.ErrUnconfirmed):
			unconfirmed = append(unconfirmed, prop.Name)
		default:
			return err
		}
	}
	if len(unconfirmed) > 0 {
		return fmt.Errorf("%w: %s", bulb.ErrUnconfirmed, strings.Join(unconfirmed, ", "))
	}
	return nil
}

// applyOne sends a single-property command and waits for the bulb to confirm it.
func (s *session) applyOne(ctx context.Context, name string, prop protocol.Property) error {
	payload, seq, err := protocol.EncodeCommand(prop.Map(), s.srv.now())
	if err != nil {
		return err
	}
	id, err := commandID(payload)
	if err != nil {
		return err
	}

	wait := &pendingCommand{id: id, seq: seq, done: make(chan error, 1)}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("bulb %s disconnected before the command was sent", name)
	}
	s.pending[id] = wait
	s.pendingSeq[seq] = wait
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		delete(s.pendingSeq, seq)
		s.mu.Unlock()
	}()

	if err := s.write(protocol.EncodePublish(s.topics.CommonService(), payload)); err != nil {
		return fmt.Errorf("sending %s to %s: %w", prop.Name, name, err)
	}
	select {
	case err := <-wait.done:
		return err
	case <-ctx.Done():
		// Delivered, but the bulb has not spoken yet. That is not a failure —
		// these bulbs have been observed confirming the same change after four
		// seconds on one occasion and nineteen on another.
		return bulb.ErrUnconfirmed
	}
}

func (s *session) write(p []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	_, err := s.conn.Write(p)
	return err
}

// serve runs the read loop until the connection closes or ctx is cancelled.
func (s *session) serve(ctx context.Context) {
	defer s.conn.Close()

	// Stop the read loop when the server shuts down.
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()

	connect, err := s.handshake()
	if errors.Is(err, errNotConfigured) {
		s.reportRejection()
		return
	}
	if err != nil {
		s.srv.publish(events.Event{
			At: s.srv.now(), Kind: events.ProtocolError,
			Detail: fmt.Sprintf("handshake from %s failed: %v", s.conn.RemoteAddr(), err),
		})
		return
	}

	s.register(connect)
	defer s.srv.reg.Disconnect(s.DeviceID(), s)

	for {
		if err := s.conn.SetReadDeadline(time.Now().Add(s.idleTimeout())); err != nil {
			return
		}
		pkt, err := protocol.ReadPacket(s.reader)
		if err != nil {
			s.reportDisconnect(err)
			return
		}
		if err := s.handle(pkt); err != nil {
			// One bad packet drops this connection and nothing else. Other
			// bulbs are unaffected (FR-016).
			s.srv.publish(s.event(events.ProtocolError, err.Error()))
			return
		}
	}
}

// handshake reads CONNECT and answers CONNACK.
func (s *session) handshake() (protocol.Connect, error) {
	if err := s.conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return protocol.Connect{}, err
	}
	pkt, err := protocol.ReadPacket(s.reader)
	if err != nil {
		return protocol.Connect{}, fmt.Errorf("reading CONNECT: %w", err)
	}
	if pkt.Type != protocol.TypeConnect {
		return protocol.Connect{}, fmt.Errorf("expected CONNECT, got packet type %d", pkt.Type)
	}
	connect, err := protocol.DecodeConnect(pkt.Payload)
	if err != nil {
		return protocol.Connect{}, err
	}
	identity, err := protocol.IdentityFromConnect(connect)
	if err != nil {
		return protocol.Connect{}, err
	}
	s.identity = identity
	s.topics = protocol.TopicsFor(identity)
	if connect.KeepAlive > 0 {
		s.keepAliv = time.Duration(connect.KeepAlive) * time.Second
	}

	// Admission is decided here and nowhere else: after the identity exists,
	// and before the CONNACK and before anything is written to the registry, so
	// a refused bulb leaves nothing behind. It is told why — MQTT 3.1.1 has a
	// return code for exactly this — rather than having its socket dropped.
	if s.srv.Admit != nil && !s.srv.Admit(identity.DeviceName) {
		_ = s.write(protocol.ConnackRefusedNotAuthorized)
		return protocol.Connect{}, errNotConfigured
	}

	// We are the authority now, so we accept the bulb's credentials rather than
	// verifying an HMAC over a vendor secret we do not have. This is the whole
	// point of the replacement server, and it is safe on the trusted LAN the
	// spec assumes.
	if err := s.write(protocol.ConnackAccepted); err != nil {
		return protocol.Connect{}, fmt.Errorf("sending CONNACK: %w", err)
	}
	return connect, nil
}

// errNotConfigured is a bulb the server has not been configured to serve. It is
// not a protocol error and must not be reported as one.
var errNotConfigured = errors.New("bulb is not in the configured lamp set")

// event builds an event tagged with this session's bulb, reading the display
// name from the registry rather than from a stale local copy.
func (s *session) event(kind events.Kind, detail string) events.Event {
	e := events.Event{At: s.srv.now(), Kind: kind, DeviceID: s.DeviceID(), Detail: detail}
	if b, ok := s.srv.reg.View(s.DeviceID()); ok {
		e.Name = b.Name
	}
	return e
}

// register puts the bulb in the registry and announces it.
func (s *session) register(connect protocol.Connect) {
	_ = connect
	now := s.srv.now()
	// Capabilities start undetermined and are filled in when the OTA inform or
	// the first property report arrives. Guessing here would be worse than
	// admitting we do not know yet.
	caps := protocol.CapabilitiesFromVersion("")
	_, isNew := s.srv.reg.Upsert(s.DeviceID(), s.conn.RemoteAddr().String(), caps, now)

	if prev := s.srv.reg.SetDriver(s.DeviceID(), s); prev != nil && prev != bulb.Driver(s) {
		// The device id already had a connection. Almost always this is the same
		// bulb coming back after a power cut: the old TCP socket is half-open
		// and the server has not learned it is dead yet, because nothing has
		// been written to it since. MQTT 3.1.1 section 3.1.4 says the newest
		// connection for a client id wins, so take over and close the old one
		// rather than reporting a duplicate device that does not exist.
		go prev.Close()
		if s.srv.noteTakeover(s.DeviceID(), now) {
			// Repeated takeovers in quick succession are the real thing the
			// duplicate warning is for: two devices presenting one id, each
			// kicking the other off in a loop.
			s.srv.publish(s.event(events.DuplicateID, s.conn.RemoteAddr().String()))
		}
	}

	kind := events.Connected
	if isNew {
		kind = events.Discovered
	}
	s.srv.publish(s.event(kind, ""))
}

// reportRejection publishes a refusal, at most once per window per device id.
// The bulb reconnects indefinitely, so reporting every attempt would drown the
// record stream in the one thing the operator already knows.
func (s *session) reportRejection() {
	now := s.srv.now()
	report, suppressed, since := s.srv.noteRejection(s.DeviceID(), now)
	if !report {
		return
	}
	detail := s.conn.RemoteAddr().String()
	if suppressed > 0 {
		detail = fmt.Sprintf("%s (%d attempts since %s)", detail, suppressed, since.Format("15:04:05"))
	}
	s.srv.publish(events.Event{
		At: now, Kind: events.Rejected, DeviceID: s.DeviceID(), Detail: detail,
	})
}

func (s *session) reportDisconnect(err error) {
	// A session that no longer owns the driver has been superseded by a newer
	// connection from the same bulb. Announcing its death would tell the
	// operator the bulb went away when it is online and answering commands.
	if s.srv.reg.Driver(s.DeviceID()) != bulb.Driver(s) {
		return
	}
	detail := "closed by bulb"
	switch {
	case errors.Is(err, io.EOF):
	case errors.Is(err, net.ErrClosed), errors.Is(err, context.Canceled):
		detail = "server shutting down"
	case isTimeout(err):
		detail = fmt.Sprintf("no keep-alive for %s", s.idleTimeout())
	case err != nil:
		detail = err.Error()
	}
	s.srv.publish(s.event(events.Disconnected, detail))
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// idleTimeout is how long silence is tolerated before the bulb is considered
// gone. MQTT convention is 1.5x the keep-alive the client declared.
func (s *session) idleTimeout() time.Duration {
	ka := s.keepAliv
	if ka <= 0 {
		ka = 120 * time.Second
	}
	return ka + ka/2
}

// handle dispatches one packet.
func (s *session) handle(pkt protocol.Packet) error {
	now := s.srv.now()
	s.srv.reg.Touch(s.DeviceID(), now)

	switch pkt.Type {
	case protocol.TypePingreq:
		return s.write(protocol.Pingresp)

	case protocol.TypeSubscribe:
		id, topics, err := protocol.DecodeSubscribe(pkt.Payload)
		if err != nil {
			return err
		}
		return s.write(protocol.EncodeSuback(id, len(topics)))

	case protocol.TypeDisconnect:
		return io.EOF

	case protocol.TypePublish:
		pub, err := protocol.DecodePublish(pkt.Payload, pkt.QoS())
		if err != nil {
			return err
		}
		if pkt.QoS() == 1 {
			if err := s.write(protocol.EncodePuback(pub.PacketID)); err != nil {
				return err
			}
		}
		return s.handlePublish(pub, now)

	case protocol.TypePuback, protocol.TypeUnsubscribe:
		return nil

	default:
		// Unknown packet types are tolerated: an unfamiliar bulb feature must
		// not drop a working connection.
		return nil
	}
}

func (s *session) handlePublish(pub protocol.Publish, now time.Time) error {
	switch {
	case strings.HasSuffix(pub.Topic, protocol.SuffixPropertyPost):
		return s.handlePropertyPost(pub, now)

	case strings.HasSuffix(pub.Topic, protocol.SuffixServiceReply):
		reply, err := protocol.DecodeServiceReply(pub.Payload)
		if err != nil {
			return err
		}
		s.completeCommand(reply)
		return nil

	case strings.Contains(pub.Topic, protocol.SuffixOTAInform):
		if version := protocol.DecodeOTAVersion(pub.Payload); version != "" {
			s.srv.reg.SetFirmware(s.DeviceID(), version)
			s.srv.reg.SetCapabilities(s.DeviceID(), protocol.CapabilitiesFromVersion(version))
		}
		return nil

	case strings.Contains(pub.Topic, "/ext/ntp/") && strings.HasSuffix(pub.Topic, protocol.SuffixNTPRequest):
		reply := protocol.EncodeNTPResponse(protocol.DecodeNTPRequest(pub.Payload), now)
		return s.write(protocol.EncodePublish(s.topics.NTPResponse(), reply))

	default:
		// deviceinfo, awss, lan prefix and log posts need no reply for the bulb
		// to keep working; they are recorded and ignored.
		return nil
	}
}

func (s *session) handlePropertyPost(pub protocol.Publish, now time.Time) error {
	post, err := protocol.DecodePropertyPost(pub.Payload)
	if err != nil {
		return err
	}
	current, ok := s.srv.reg.View(s.DeviceID())
	if !ok {
		return fmt.Errorf("bulb %s is no longer registered", s.DeviceID())
	}
	s.srv.reg.SetCapabilities(s.DeviceID(), protocol.RefineFromReport(current.Capabilities, post))

	next := post.Apply(current.State, now)
	changes := s.srv.reg.SetState(s.DeviceID(), next, now)

	// A report that changes nothing still matters once per connection. Anything
	// waiting to hear that a bulb is really there — Home Assistant's
	// availability, in particular — needs the confirmation, and a bulb whose
	// state survived a restart unchanged would otherwise never send one.
	s.mu.Lock()
	first := !s.reported
	s.reported = true
	s.mu.Unlock()

	if len(changes) > 0 || first {
		e := s.event(events.StateChanged, "")
		e.At, e.Changed = now, changes
		s.srv.publish(e)
	}
	s.completeBySeq(post.Seq)

	if post.ID != "" {
		reply := protocol.EncodePropertyPostReply(post.ID)
		return s.write(protocol.EncodePublish(s.topics.PropertyPostReply(), reply))
	}
	return nil
}

// completeCommand releases whoever is waiting on this command's acknowledgement.
func (s *session) completeCommand(reply protocol.ServiceReply) {
	s.mu.Lock()
	wait, ok := s.pending[reply.ID]
	s.mu.Unlock()
	if !ok {
		return
	}
	var err error
	if reply.Code != 200 {
		err = fmt.Errorf("bulb rejected the command with code %d", reply.Code)
	}
	deliver(wait, err)
}

// completeBySeq releases a command confirmed by the bulb reporting the state it
// produced. The bulb echoes the command's seq in CommonServiceResponse, and a
// state report is stronger evidence than an ack: it says the change happened,
// not merely that the message arrived.
func (s *session) completeBySeq(seq string) {
	if seq == "" {
		return
	}
	s.mu.Lock()
	wait, ok := s.pendingSeq[seq]
	s.mu.Unlock()
	if ok {
		deliver(wait, nil)
	}
}

// deliver completes a pending command exactly once. The buffered channel and
// the default case together mean the second confirmation to arrive is dropped
// rather than blocking whichever goroutine carries it.
func deliver(wait *pendingCommand, err error) {
	select {
	case wait.done <- err:
	default:
	}
}
