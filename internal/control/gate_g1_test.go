package control

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
	"github.com/mmichaelb/haigosmart/internal/bulb/fakebulb"
)

// Gate G1. The refactor moved the logic into internal/lights; the terminal's
// output must be untouched.
//
// The gate was relaxed to allow adapting tests to the moved API, which removed
// the frozen-suite safety net. This is the replacement: every output line here
// is quoted verbatim from feature 001's contracts/tui-commands.md, so what is
// being checked is the contract rather than whatever the tests happen to say
// after being edited.
func TestGateG1ContractStringsUnchanged(t *testing.T) {
	h := newHarness(t, fakebulb.Options{DeviceName: "a41f2c"})

	// Adoption, quoted from the contract's Adoption section.
	if got := h.run("on a41f2c").Line(); got != "error   a41f2c: not adopted yet. run `name a41f2c <a-name>` first" {
		t.Errorf("adoption refusal:\n got %q", got)
	}
	if got := h.run("name a41f2c kitchen").Line(); got != "ok      kitchen: adopted (was a41f2c)" {
		t.Errorf("adoption:\n got %q", got)
	}
	if got := h.run("name kitchen kitchen-1").Line(); got != "ok      kitchen-1: renamed from kitchen" {
		t.Errorf("rename:\n got %q", got)
	}
	if got := h.run("name kitchen-1 kitchen").Line(); got != "ok      kitchen: renamed from kitchen-1" {
		t.Errorf("rename back:\n got %q", got)
	}

	waitFor(t, "the lamp's initial report", func() bool {
		b, _ := h.reg.View("a41f2c")
		return b.State.Brightness == 30
	})

	tests := []struct {
		name string
		line string
		want string
	}{
		{"power on", "on kitchen", "ok      kitchen: on"},
		{"brightness", "bri kitchen 80", "ok      kitchen: brightness 80"},
		{
			"unknown bulb", "on kitchn",
			`error   unknown bulb "kitchn": no bulb by that name or id. run ` + "`list`" + ` to see registered bulbs`,
		},
		{"brightness out of range", "bri kitchen 150", "error   brightness must be 0-100, got 150"},
		{
			"unknown command", "dim kitchen",
			`error   unknown command "dim". commands: list on off bri color temp name info help quit`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.run(tc.line).Line(); got != tc.want {
				t.Errorf("contract string changed\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// The two contract strings that need a second lamp or a specific capability.
func TestGateG1ContractStringsNeedingSetup(t *testing.T) {
	h := newHarness(t, fakebulb.Options{DeviceName: "aaa"})
	if res := h.run("name aaa kitchen"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	h.reg.Upsert("bbb", "addr", bulb.Capabilities{Known: true, MinBrightness: 1}, time.Now())
	if _, err := h.reg.Rename("bbb", "kids-room"); err != nil {
		t.Fatal(err)
	}

	want := `error   ambiguous target "k": matches kids-room, kitchen. use a longer prefix`
	if got := h.run("on k").Line(); got != want {
		t.Errorf("ambiguity\n got %q\nwant %q", got, want)
	}

	// A lamp with no colour, named to match the contract's example.
	h.reg.Upsert("hallid", "addr", bulb.Capabilities{Known: true, ColorTemp: true, MinBrightness: 1}, time.Now())
	if _, err := h.reg.Rename("hallid", "hall"); err != nil {
		t.Fatal(err)
	}
	want = "error   hall: does not support colour. this bulb accepts `on`, `off`, `bri`, `temp`"
	if got := h.run("color hall red").Line(); got != want {
		t.Errorf("unsupported colour\n got %q\nwant %q", got, want)
	}
}

// The not-connected string carries a timestamp, so it is matched by shape.
func TestGateG1NotConnectedString(t *testing.T) {
	h := newHarness(t, fakebulb.Options{DeviceName: "desk"})
	if res := h.run("name desk desk"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	h.fb.Close()
	waitFor(t, "the disconnect", func() bool {
		b, _ := h.reg.View("desk")
		return b.Status == bulb.Disconnected
	})
	got := h.run("on desk").Line()
	want := regexp.MustCompile(`^error   desk: not connected \(last seen \d\d:\d\d:\d\d\)\. check the bulb has power$`)
	if !want.MatchString(got) {
		t.Errorf("not-connected string changed\n got %q\nwant the contract's shape", got)
	}
}

// The three output shapes themselves are part of the contract.
func TestGateG1OutputShapes(t *testing.T) {
	for _, tc := range []struct {
		res  Result
		want string
	}{
		{Result{Kind: ResultOK, Text: "x"}, "ok      x"},
		{Result{Kind: ResultError, Text: "x"}, "error   x"},
		{Result{Kind: ResultInfo, Text: "x"}, "info    x"},
	} {
		if got := tc.res.Line(); got != tc.want {
			t.Errorf("output shape changed: %q, want %q", got, tc.want)
		}
	}
}

// Every verb the contract lists must still parse.
func TestGateG1VerbsUnchanged(t *testing.T) {
	want := "list on off bri color temp name info help quit"
	if got := strings.Join(Verbs, " "); got != want {
		t.Errorf("verb list changed\n got %q\nwant %q", got, want)
	}
}
