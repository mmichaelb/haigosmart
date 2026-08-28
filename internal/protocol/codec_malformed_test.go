package protocol

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
)

// A misbehaving bulb must never take the server down: every malformed input
// returns an error and none of them panic (FR-016).
func TestReadPacketMalformed(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{"empty", nil},
		{"header only", []byte{0x10}},
		{"truncated body", []byte{0x10, 0x20, 0x01, 0x02}},
		{"remaining length never terminates", []byte{0x10, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{"length exceeds maximum", []byte{0x30, 0xff, 0xff, 0xff, 0x7f}},
		{"garbage", []byte{0xff, 0xff, 0xff}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ReadPacket(bufio.NewReader(bytes.NewReader(tc.raw))); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestReadPacketRejectsOversizeBeforeAllocating(t *testing.T) {
	// 0xff 0xff 0xff 0x7f is the maximum MQTT remaining length, ~256 MiB.
	// Rejecting it by size rather than by trying to read it is the point.
	_, err := ReadPacket(bufio.NewReader(bytes.NewReader([]byte{0x30, 0xff, 0xff, 0xff, 0x7f})))
	if !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("err = %v, want ErrPacketTooLarge", err)
	}
}

func TestDecodersMalformed(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]byte) error
	}{
		{"connect", func(b []byte) error { _, err := DecodeConnect(b); return err }},
		{"publish", func(b []byte) error { _, err := DecodePublish(b, 1); return err }},
		{"subscribe", func(b []byte) error { _, _, err := DecodeSubscribe(b); return err }},
		{"property post", func(b []byte) error { _, err := DecodePropertyPost(b); return err }},
		{"service reply", func(b []byte) error { _, err := DecodeServiceReply(b); return err }},
	}
	inputs := [][]byte{
		nil,
		{0x00},
		{0x00, 0xff},
		{0xff, 0xff, 0xff, 0xff},
		[]byte("not json at all"),
		[]byte(`{"params":`),
	}
	for _, tc := range tests {
		for i, in := range inputs {
			t.Run(tc.name, func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("input %d panicked: %v", i, r)
					}
				}()
				_ = tc.fn(in) // an error is fine; a panic is not
			})
		}
	}
}

func TestDecodePropertyPostIgnoresUnknownProperties(t *testing.T) {
	// The bulb reports LightScene, WorkMode and LightType, which we do not model.
	// Unknown properties must be skipped, not rejected.
	post, err := DecodePropertyPost([]byte(`{"id":"5","params":{"LightType":1,"WorkMode":0,"Brightness":42,"LightScene":{"SceneId":"0"}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post.Brightness == nil || *post.Brightness != 42 {
		t.Errorf("brightness = %v, want 42", post.Brightness)
	}
}

func TestClampPercentOutOfRange(t *testing.T) {
	tests := []struct {
		in   float64
		want uint8
	}{{-5, 0}, {0, 0}, {50, 50}, {100, 100}, {255, 100}, {1e9, 100}}
	for _, tc := range tests {
		if got := clampPercent(tc.in); got != tc.want {
			t.Errorf("clampPercent(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
