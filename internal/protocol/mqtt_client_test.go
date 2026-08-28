package protocol

import (
	"bufio"
	"bytes"
	"testing"
)

func TestEncodeConnectRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		opts ConnectOptions
	}{
		{
			name: "minimal",
			opts: ConnectOptions{ClientID: "haigosmart", KeepAlive: 60},
		},
		{
			name: "credentials",
			opts: ConnectOptions{ClientID: "haigosmart", KeepAlive: 60, Username: "ha", Password: "s3cret"},
		},
		{
			name: "will only",
			opts: ConnectOptions{
				ClientID: "haigosmart", KeepAlive: 60,
				WillTopic: "haigosmart/status", WillPayload: []byte("offline"), WillRetain: true,
			},
		},
		{
			name: "will and credentials together",
			opts: ConnectOptions{
				ClientID: "haigosmart", KeepAlive: 120, CleanSession: true,
				Username: "ha", Password: "s3cret",
				WillTopic: "haigosmart/status", WillPayload: []byte("offline"), WillRetain: true, WillQoS: 1,
			},
		},
		{
			name: "empty password is omitted, not sent blank",
			opts: ConnectOptions{ClientID: "haigosmart", KeepAlive: 60, Username: "ha"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := EncodeConnect(tc.opts)
			pkt, err := ReadPacket(bufio.NewReader(bytes.NewReader(raw)))
			if err != nil {
				t.Fatalf("ReadPacket: %v", err)
			}
			if pkt.Type != TypeConnect {
				t.Fatalf("type = %d, want CONNECT", pkt.Type)
			}
			// The server-side decoder is the real check: what we encode must be
			// what a broker parses.
			got, err := DecodeConnect(pkt.Payload)
			if err != nil {
				t.Fatalf("DecodeConnect: %v", err)
			}
			if got.ClientID != tc.opts.ClientID {
				t.Errorf("client id = %q, want %q", got.ClientID, tc.opts.ClientID)
			}
			if got.KeepAlive != tc.opts.KeepAlive {
				t.Errorf("keep-alive = %d, want %d", got.KeepAlive, tc.opts.KeepAlive)
			}
			if got.Username != tc.opts.Username {
				t.Errorf("username = %q, want %q", got.Username, tc.opts.Username)
			}
			if got.Password != tc.opts.Password {
				t.Errorf("password = %q, want %q", got.Password, tc.opts.Password)
			}
		})
	}
}

// A will has to survive encoding intact, because it is the only thing that
// reports a crash. Getting its position in the packet wrong would silently
// corrupt the username that follows it.
func TestEncodeConnectWillDoesNotCorruptCredentials(t *testing.T) {
	raw := EncodeConnect(ConnectOptions{
		ClientID: "haigosmart", KeepAlive: 60,
		WillTopic: "haigosmart/status", WillPayload: []byte("offline"), WillRetain: true,
		Username: "ha", Password: "s3cret",
	})
	pkt, err := ReadPacket(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeConnect(pkt.Payload)
	if err != nil {
		t.Fatalf("DecodeConnect: %v", err)
	}
	if got.Username != "ha" || got.Password != "s3cret" {
		t.Errorf("credentials after a will = %q/%q, want ha/s3cret", got.Username, got.Password)
	}
}

func TestEncodeSubscribeRoundTrip(t *testing.T) {
	raw := EncodeSubscribe(7, 1, "a/b", "c/d/e")
	pkt, err := ReadPacket(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != TypeSubscribe {
		t.Fatalf("type = %d, want SUBSCRIBE", pkt.Type)
	}
	if pkt.Flags != 2 {
		t.Errorf("flags = %d, want the reserved 2 (MQTT 3.1.1 §3.8.1)", pkt.Flags)
	}
	id, topics, err := DecodeSubscribe(pkt.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if id != 7 {
		t.Errorf("packet id = %d, want 7", id)
	}
	if len(topics) != 2 || topics[0] != "a/b" || topics[1] != "c/d/e" {
		t.Errorf("topics = %v", topics)
	}
}

func TestDecodeConnack(t *testing.T) {
	tests := []struct {
		name        string
		payload     []byte
		wantPresent bool
		wantCode    byte
		wantErr     bool
	}{
		{name: "accepted", payload: []byte{0, 0}, wantCode: ConnackAcceptedCode},
		{name: "session present", payload: []byte{1, 0}, wantPresent: true},
		{name: "bad credentials", payload: []byte{0, 4}, wantCode: ConnackBadCredentials},
		{name: "truncated", payload: []byte{0}, wantErr: true},
		{name: "empty", payload: nil, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			present, code, err := DecodeConnack(tc.payload)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if present != tc.wantPresent || code != tc.wantCode {
				t.Errorf("got present=%v code=%d, want %v/%d", present, code, tc.wantPresent, tc.wantCode)
			}
		})
	}
}

// A rejected connection must be reportable as what it is. "Connection refused"
// and "your password is wrong" are different problems for whoever is reading
// the log.
func TestConnackReasonIsDiagnostic(t *testing.T) {
	tests := []struct {
		code byte
		want string
	}{
		{ConnackAcceptedCode, "accepted"},
		{ConnackBadCredentials, "broker rejected the username or password"},
		{ConnackIdentifierRejected, "broker rejected the client id"},
		{ConnackNotAuthorized, "not authorised by the broker"},
		{99, "broker refused the connection with code 99"},
	}
	for _, tc := range tests {
		if got := ConnackReason(tc.code); got != tc.want {
			t.Errorf("ConnackReason(%d) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestDecodeSuback(t *testing.T) {
	raw := EncodeSuback(9, 2)
	pkt, err := ReadPacket(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	id, granted, err := DecodeSuback(pkt.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if id != 9 || len(granted) != 2 {
		t.Errorf("id=%d granted=%v, want 9 and two entries", id, granted)
	}
	if _, _, err := DecodeSuback([]byte{0, 1}); err == nil {
		t.Error("a suback with no granted codes should be an error")
	}
}

func TestEncodePublishVariants(t *testing.T) {
	tests := []struct {
		name       string
		raw        []byte
		wantQoS    byte
		wantRetain bool
		wantID     uint16
	}{
		{name: "qos 0 plain", raw: EncodePublish("a/b", []byte("x"))},
		{name: "qos 0 retained", raw: EncodePublishRetained("a/b", []byte("x")), wantRetain: true},
		{name: "qos 1", raw: EncodePublishQoS1("a/b", []byte("x"), 42, false), wantQoS: 1, wantID: 42},
		{name: "qos 1 retained", raw: EncodePublishQoS1("a/b", []byte("x"), 43, true), wantQoS: 1, wantRetain: true, wantID: 43},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkt, err := ReadPacket(bufio.NewReader(bytes.NewReader(tc.raw)))
			if err != nil {
				t.Fatal(err)
			}
			if pkt.QoS() != tc.wantQoS {
				t.Errorf("qos = %d, want %d", pkt.QoS(), tc.wantQoS)
			}
			if pkt.Retained() != tc.wantRetain {
				t.Errorf("retained = %v, want %v", pkt.Retained(), tc.wantRetain)
			}
			pub, err := DecodePublish(pkt.Payload, pkt.QoS())
			if err != nil {
				t.Fatal(err)
			}
			if pub.Topic != "a/b" || string(pub.Payload) != "x" {
				t.Errorf("decoded %q / %q", pub.Topic, pub.Payload)
			}
			if pub.PacketID != tc.wantID {
				t.Errorf("packet id = %d, want %d", pub.PacketID, tc.wantID)
			}
		})
	}
}

func TestEncodeDisconnect(t *testing.T) {
	pkt, err := ReadPacket(bufio.NewReader(bytes.NewReader(EncodeDisconnect())))
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != TypeDisconnect || len(pkt.Payload) != 0 {
		t.Errorf("got type %d with %d payload bytes", pkt.Type, len(pkt.Payload))
	}
}
