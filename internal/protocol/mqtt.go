// Package protocol implements the wire protocol the Aigo bulbs already speak:
// MQTT 3.1.1 over plain TCP carrying Alibaba Cloud IoT ("Alink") JSON payloads.
//
// The full contract, derived from a packet capture of real hardware, is in
// specs/001-local-bulb-server/contracts/bulb-protocol.md.
package protocol

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MQTT control packet types (MQTT 3.1.1, section 2.2.1).
const (
	TypeConnect     = 1
	TypeConnack     = 2
	TypePublish     = 3
	TypePuback      = 4
	TypeSubscribe   = 8
	TypeSuback      = 9
	TypeUnsubscribe = 10
	TypeUnsuback    = 11
	TypePingreq     = 12
	TypePingresp    = 13
	TypeDisconnect  = 14
)

// MaxPacketSize bounds a single control packet. The bulbs' largest observed
// packet is under 500 bytes; 1 MiB is generous and stops a malformed remaining
// length from turning into an allocation the size of memory.
const MaxPacketSize = 1 << 20

// ErrPacketTooLarge is returned when a packet's declared length exceeds
// MaxPacketSize.
var ErrPacketTooLarge = errors.New("mqtt: packet exceeds maximum size")

// Packet is one decoded MQTT control packet.
type Packet struct {
	Type    byte
	Flags   byte
	Payload []byte
}

// QoS extracts the quality-of-service level from a PUBLISH packet's flags.
func (p Packet) QoS() byte { return (p.Flags >> 1) & 3 }

// ReadPacket reads one control packet from r.
func ReadPacket(r *bufio.Reader) (Packet, error) {
	head, err := r.ReadByte()
	if err != nil {
		return Packet{}, err
	}
	length, err := readVarint(r)
	if err != nil {
		return Packet{}, err
	}
	if length > MaxPacketSize {
		return Packet{}, fmt.Errorf("%w: %d bytes", ErrPacketTooLarge, length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return Packet{}, fmt.Errorf("mqtt: reading %d byte body: %w", length, err)
	}
	return Packet{Type: head >> 4, Flags: head & 0x0f, Payload: body}, nil
}

// Encode renders a control packet for the wire.
func Encode(typ, flags byte, payload []byte) []byte {
	out := make([]byte, 0, len(payload)+5)
	out = append(out, typ<<4|flags)
	out = appendVarint(out, len(payload))
	return append(out, payload...)
}

func readVarint(r *bufio.Reader) (int, error) {
	var value, multiplier int = 0, 1
	for i := 0; i < 4; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, fmt.Errorf("mqtt: reading remaining length: %w", err)
		}
		value += int(b&127) * multiplier
		if b&128 == 0 {
			return value, nil
		}
		multiplier *= 128
	}
	return 0, errors.New("mqtt: malformed remaining length")
}

func appendVarint(dst []byte, n int) []byte {
	for {
		digit := byte(n % 128)
		n /= 128
		if n > 0 {
			digit |= 128
		}
		dst = append(dst, digit)
		if n == 0 {
			return dst
		}
	}
}

// readString reads a length-prefixed UTF-8 string from b at offset i.
func readString(b []byte, i int) (string, int, error) {
	if i+2 > len(b) {
		return "", 0, errors.New("mqtt: truncated string length")
	}
	n := int(binary.BigEndian.Uint16(b[i:]))
	if i+2+n > len(b) {
		return "", 0, fmt.Errorf("mqtt: string of %d bytes exceeds packet", n)
	}
	return string(b[i+2 : i+2+n]), i + 2 + n, nil
}

func appendString(dst []byte, s string) []byte {
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(s)))
	return append(dst, s...)
}

// Connect is the subset of a CONNECT packet the server cares about.
type Connect struct {
	ProtocolName  string
	ProtocolLevel byte
	KeepAlive     uint16
	ClientID      string
	Username      string
	Password      string
}

// DecodeConnect parses a CONNECT packet body.
func DecodeConnect(payload []byte) (Connect, error) {
	var c Connect
	name, i, err := readString(payload, 0)
	if err != nil {
		return c, fmt.Errorf("mqtt: connect protocol name: %w", err)
	}
	c.ProtocolName = name
	if i+4 > len(payload) {
		return c, errors.New("mqtt: connect header truncated")
	}
	c.ProtocolLevel = payload[i]
	flags := payload[i+1]
	c.KeepAlive = binary.BigEndian.Uint16(payload[i+2:])
	i += 4

	if c.ClientID, i, err = readString(payload, i); err != nil {
		return c, fmt.Errorf("mqtt: connect client id: %w", err)
	}
	if flags&0x04 != 0 { // will flag: skip topic and message
		if _, i, err = readString(payload, i); err != nil {
			return c, fmt.Errorf("mqtt: connect will topic: %w", err)
		}
		if _, i, err = readString(payload, i); err != nil {
			return c, fmt.Errorf("mqtt: connect will message: %w", err)
		}
	}
	if flags&0x80 != 0 {
		if c.Username, i, err = readString(payload, i); err != nil {
			return c, fmt.Errorf("mqtt: connect username: %w", err)
		}
	}
	if flags&0x40 != 0 {
		if c.Password, _, err = readString(payload, i); err != nil {
			return c, fmt.Errorf("mqtt: connect password: %w", err)
		}
	}
	return c, nil
}

// Publish is a decoded PUBLISH packet.
type Publish struct {
	Topic    string
	PacketID uint16
	Payload  []byte
}

// DecodePublish parses a PUBLISH packet body. qos comes from the fixed header.
func DecodePublish(payload []byte, qos byte) (Publish, error) {
	var p Publish
	topic, i, err := readString(payload, 0)
	if err != nil {
		return p, fmt.Errorf("mqtt: publish topic: %w", err)
	}
	p.Topic = topic
	if qos > 0 {
		if i+2 > len(payload) {
			return p, errors.New("mqtt: publish packet id truncated")
		}
		p.PacketID = binary.BigEndian.Uint16(payload[i:])
		i += 2
	}
	p.Payload = payload[i:]
	return p, nil
}

// EncodePublish renders a QoS 0 PUBLISH.
func EncodePublish(topic string, payload []byte) []byte {
	body := appendString(make([]byte, 0, len(topic)+len(payload)+2), topic)
	return Encode(TypePublish, 0, append(body, payload...))
}

// DecodeSubscribe returns the topic filters and the packet id of a SUBSCRIBE.
func DecodeSubscribe(payload []byte) (packetID uint16, topics []string, err error) {
	if len(payload) < 2 {
		return 0, nil, errors.New("mqtt: subscribe packet id truncated")
	}
	packetID = binary.BigEndian.Uint16(payload)
	for i := 2; i < len(payload); {
		var topic string
		if topic, i, err = readString(payload, i); err != nil {
			return 0, nil, fmt.Errorf("mqtt: subscribe filter: %w", err)
		}
		if i >= len(payload) {
			return 0, nil, errors.New("mqtt: subscribe missing requested qos")
		}
		i++ // requested QoS byte
		topics = append(topics, topic)
	}
	return packetID, topics, nil
}

// Prebuilt responses. These never vary, so there is nothing to allocate per use.
var (
	// ConnackAccepted is CONNACK with session-present 0 and return code 0.
	ConnackAccepted = []byte{TypeConnack << 4, 2, 0, 0}
	// Pingresp answers a PINGREQ.
	Pingresp = []byte{TypePingresp << 4, 0}
)

// EncodeSuback grants QoS 0 for each of n requested topic filters.
func EncodeSuback(packetID uint16, n int) []byte {
	body := binary.BigEndian.AppendUint16(make([]byte, 0, 2+n), packetID)
	for range n {
		body = append(body, 0)
	}
	return Encode(TypeSuback, 0, body)
}

// EncodePuback acknowledges a QoS 1 PUBLISH.
func EncodePuback(packetID uint16) []byte {
	return Encode(TypePuback, 0, binary.BigEndian.AppendUint16(nil, packetID))
}
