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

	"github.com/mmichaelb/haigosmart/internal/bulb"
)

func benchFixture(b *testing.B, name string) []byte {
	b.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".hex"))
	if err != nil {
		b.Fatal(err)
	}
	out, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		b.Fatal(err)
	}
	return out
}

// BenchmarkReadPacket is the hottest path in the server: every frame from every
// bulb goes through it.
func BenchmarkReadPacket(b *testing.B) {
	raw := benchFixture(b, "c2s_property_post_brightness_step6")
	r := bytes.NewReader(raw)
	br := bufio.NewReader(r)
	b.ReportAllocs()
	for b.Loop() {
		r.Reset(raw)
		br.Reset(r)
		if _, err := ReadPacket(br); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodePropertyPost(b *testing.B) {
	raw := benchFixture(b, "c2s_property_post_brightness_step6")
	pkt, err := ReadPacket(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		b.Fatal(err)
	}
	pub, err := DecodePublish(pkt.Payload, pkt.QoS())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DecodePropertyPost(pub.Payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeConnect(b *testing.B) {
	raw := benchFixture(b, "c2s_connect_step1")
	pkt, err := ReadPacket(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DecodeConnect(pkt.Payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeCommand(b *testing.B) {
	props := map[string]any{"LightSwitch": 1, "Brightness": 80, "ColorTemperature": 50}
	now := time.Now()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := EncodeCommand(props, now); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodePublish(b *testing.B) {
	topic := "/sys/a1GGnyln558/703e975dc388/thing/service/CommonService"
	payload := []byte(`{"method":"thing.service.CommonService","id":"1","params":{}}`)
	b.ReportAllocs()
	for b.Loop() {
		_ = EncodePublish(topic, payload)
	}
}

func BenchmarkStateDiff(b *testing.B) {
	before := bulb.LightState{Power: true, Brightness: 40, ColorTemp: 20}
	after := bulb.LightState{Power: true, Brightness: 80, ColorTemp: 50}
	b.ReportAllocs()
	for b.Loop() {
		_ = before.Diff(after)
	}
}
