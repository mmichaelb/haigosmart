package protocol

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"haigosmart/internal/bulb"
)

// fixture loads a captured packet from testdata. These bytes came off real
// hardware, so a test that passes here is a test that agrees with the bulb
// rather than with our reading of it.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".hex"))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("decoding fixture %s: %v", name, err)
	}
	return b
}

func readOne(t *testing.T, raw []byte) Packet {
	t.Helper()
	p, err := ReadPacket(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	return p
}

func TestReadPacketFixtures(t *testing.T) {
	tests := []struct {
		fixture  string
		wantType byte
	}{
		{"c2s_connect_step1", TypeConnect},
		{"s2c_connack_step1", TypeConnack},
		{"c2s_subscribe_step1", TypeSubscribe},
		{"s2c_suback_step1", TypeSuback},
		{"c2s_pingreq_step1", TypePingreq},
		{"s2c_pingresp_step1", TypePingresp},
		{"s2c_puback_step1", TypePuback},
		{"c2s_property_post_initial_step1", TypePublish},
		{"s2c_commonservice_lightswitch_step4", TypePublish},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			raw := fixture(t, tc.fixture)
			p := readOne(t, raw)
			if p.Type != tc.wantType {
				t.Errorf("type = %d, want %d", p.Type, tc.wantType)
			}
			// Re-encoding must reproduce the captured bytes exactly.
			if got := Encode(p.Type, p.Flags, p.Payload); !bytes.Equal(got, raw) {
				t.Errorf("round trip mismatch\n got %x\nwant %x", got, raw)
			}
		})
	}
}

func TestDecodeConnect(t *testing.T) {
	p := readOne(t, fixture(t, "c2s_connect_step1"))
	c, err := DecodeConnect(p.Payload)
	if err != nil {
		t.Fatalf("DecodeConnect: %v", err)
	}
	if c.ProtocolName != "MQTT" || c.ProtocolLevel != 4 {
		t.Errorf("protocol = %q level %d, want MQTT level 4", c.ProtocolName, c.ProtocolLevel)
	}
	if c.KeepAlive != 120 {
		t.Errorf("keep-alive = %d, want 120", c.KeepAlive)
	}
	if c.Username != "703e975dc388&a1GGnyln558" {
		t.Errorf("username = %q", c.Username)
	}
	if c.Password == "" {
		t.Error("password not parsed")
	}
}

func TestIdentityFromConnect(t *testing.T) {
	tests := []struct {
		name    string
		connect Connect
		wantID  string
		wantKey string
		wantErr bool
	}{
		{
			name:    "from username",
			connect: Connect{Username: "703e975dc388&a1GGnyln558"},
			wantID:  "703e975dc388", wantKey: "a1GGnyln558",
		},
		{
			name:    "falls back to client id",
			connect: Connect{ClientID: "a1GGnyln558.703e975dc388|securemode=2,token=x|"},
			wantID:  "703e975dc388", wantKey: "a1GGnyln558",
		},
		{name: "neither", connect: Connect{}, wantErr: true},
		{name: "malformed username", connect: Connect{Username: "nodelimiter"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, err := IdentityFromConnect(tc.connect)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id.DeviceName != tc.wantID || id.ProductKey != tc.wantKey {
				t.Errorf("got %s/%s, want %s/%s", id.ProductKey, id.DeviceName, tc.wantKey, tc.wantID)
			}
		})
	}
}

func TestDecodePropertyPostInitial(t *testing.T) {
	p := readOne(t, fixture(t, "c2s_property_post_initial_step1"))
	pub, err := DecodePublish(p.Payload, p.QoS())
	if err != nil {
		t.Fatalf("DecodePublish: %v", err)
	}
	if !strings.HasSuffix(pub.Topic, SuffixPropertyPost) {
		t.Fatalf("topic = %q", pub.Topic)
	}
	post, err := DecodePropertyPost(pub.Payload)
	if err != nil {
		t.Fatalf("DecodePropertyPost: %v", err)
	}
	// The initial report uses bare scalars: LightSwitch 1, Brightness 30,
	// ColorTemperature 2.
	if post.Power == nil || !*post.Power {
		t.Error("power should be on")
	}
	if post.Brightness == nil || *post.Brightness != 30 {
		t.Errorf("brightness = %v, want 30", post.Brightness)
	}
	if post.ColorTemp == nil || *post.ColorTemp != 2 {
		t.Errorf("color temp = %v, want 2", post.ColorTemp)
	}
}

func TestDecodePropertyPostDeltas(t *testing.T) {
	tests := []struct {
		fixture string
		check   func(*testing.T, PropertyPost)
	}{
		{"c2s_property_post_lightswitch_step4", func(t *testing.T, p PropertyPost) {
			if p.Power == nil {
				t.Fatal("power not reported")
			}
			if p.Brightness != nil || p.ColorTemp != nil {
				t.Error("delta reported fields it did not carry")
			}
		}},
		{"c2s_property_post_brightness_step6", func(t *testing.T, p PropertyPost) {
			if p.Brightness == nil {
				t.Fatal("brightness not reported")
			}
			if p.Power != nil {
				t.Error("delta reported power it did not carry")
			}
		}},
		{"c2s_property_post_colortemperature_step13", func(t *testing.T, p PropertyPost) {
			if p.ColorTemp == nil {
				t.Fatal("color temp not reported")
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			p := readOne(t, fixture(t, tc.fixture))
			pub, err := DecodePublish(p.Payload, p.QoS())
			if err != nil {
				t.Fatalf("DecodePublish: %v", err)
			}
			post, err := DecodePropertyPost(pub.Payload)
			if err != nil {
				t.Fatalf("DecodePropertyPost: %v", err)
			}
			// Every delta echoes the seq of the command that caused it.
			if post.Seq == "" {
				t.Error("seq not echoed")
			}
			tc.check(t, post)
		})
	}
}

func TestPropertyPostApplyIsPartial(t *testing.T) {
	prev := bulb.LightState{Power: true, Brightness: 30, ColorTemp: 2}
	bright := uint8(100)
	next := PropertyPost{Brightness: &bright}.Apply(prev, time.Unix(0, 0))
	if next.Brightness != 100 {
		t.Errorf("brightness = %d, want 100", next.Brightness)
	}
	if !next.Power || next.ColorTemp != 2 {
		t.Error("a delta overwrote fields it did not carry")
	}
}

func TestEncodeCommandIsDoubleEncoded(t *testing.T) {
	payload, seq, err := EncodeCommand(map[string]any{"LightSwitch": 1}, time.UnixMilli(1787788228900))
	if err != nil {
		t.Fatalf("EncodeCommand: %v", err)
	}
	if seq != "10000@1787788228900" {
		t.Errorf("seq = %q", seq)
	}
	// params.params must be a JSON *string*, matching the vendor cloud.
	if !bytes.Contains(payload, []byte(`"params":"{\"LightSwitch\":1}"`)) {
		t.Errorf("params not double-encoded: %s", payload)
	}
}

func TestDecodeServiceReply(t *testing.T) {
	p := readOne(t, fixture(t, "c2s_commonservice_reply_step4"))
	pub, err := DecodePublish(p.Payload, p.QoS())
	if err != nil {
		t.Fatalf("DecodePublish: %v", err)
	}
	reply, err := DecodeServiceReply(pub.Payload)
	if err != nil {
		t.Fatalf("DecodeServiceReply: %v", err)
	}
	if reply.Code != 200 || reply.ID == "" {
		t.Errorf("reply = %+v, want code 200 with an id", reply)
	}
}

func TestDecodeSubscribeFixture(t *testing.T) {
	p := readOne(t, fixture(t, "c2s_subscribe_step1"))
	id, topics, err := DecodeSubscribe(p.Payload)
	if err != nil {
		t.Fatalf("DecodeSubscribe: %v", err)
	}
	if id == 0 || len(topics) != 1 {
		t.Fatalf("id=%d topics=%v", id, topics)
	}
	if !strings.HasSuffix(topics[0], "/thing/event/+/post_reply") {
		t.Errorf("topic = %q", topics[0])
	}
}
