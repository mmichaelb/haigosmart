package control

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"haigosmart/internal/bulb"
	"haigosmart/internal/bulb/fakebulb"
	"haigosmart/internal/events"
	"haigosmart/internal/lights"
	"haigosmart/internal/registry"
	"haigosmart/internal/server"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    Command
		wantErr string
	}{
		{name: "list", line: "list", want: Command{Action: ActionList}},
		{name: "on", line: "on kitchen", want: Command{Action: ActionOn, Target: "kitchen"}},
		{name: "off", line: "off kitchen", want: Command{Action: ActionOff, Target: "kitchen"}},
		{name: "brightness", line: "bri kitchen 80", want: Command{Action: ActionBrightness, Target: "kitchen", Number: 80}},
		{name: "temperature", line: "temp kitchen 50", want: Command{Action: ActionColorTemp, Target: "kitchen", Number: 50}},
		{name: "colour", line: "color kitchen #ff0000", want: Command{Action: ActionColor, Target: "kitchen", Text: "#ff0000"}},
		{name: "british spelling", line: "colour kitchen red", want: Command{Action: ActionColor, Target: "kitchen", Text: "red"}},
		{name: "name", line: "name aa11 kitchen", want: Command{Action: ActionName, Target: "aa11", Text: "kitchen"}},
		{name: "info", line: "info kitchen", want: Command{Action: ActionInfo, Target: "kitchen"}},
		{name: "help", line: "help", want: Command{Action: ActionHelp}},
		{name: "help one verb", line: "help bri", want: Command{Action: ActionHelp, Text: "bri"}},
		{name: "quit", line: "quit", want: Command{Action: ActionQuit}},
		{name: "case insensitive verb", line: "ON kitchen", want: Command{Action: ActionOn, Target: "kitchen"}},
		{name: "extra whitespace", line: "  on   kitchen  ", want: Command{Action: ActionOn, Target: "kitchen"}},

		{name: "unknown verb", line: "dim kitchen", wantErr: `unknown command "dim"`},
		{name: "on with no target", line: "on", wantErr: "on needs exactly one bulb"},
		{name: "on with two targets", line: "on a b", wantErr: "on needs exactly one bulb"},
		{name: "brightness with no value", line: "bri kitchen", wantErr: "bri needs a bulb and a value"},
		{name: "brightness not a number", line: "bri kitchen bright", wantErr: "brightness must be a whole number"},
		{name: "name with no name", line: "name kitchen", wantErr: "name needs a bulb and a new name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.line)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("err = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse("   "); err != ErrEmpty {
		t.Errorf("err = %v, want ErrEmpty", err)
	}
}

func TestValidColor(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"#ff0000", true}, {"#FFFFFF", true}, {"red", true}, {"WarmWhite", true},
		{"#fff", false}, {"#gggggg", false}, {"chartreuse", false}, {"", false},
	}
	for _, tc := range tests {
		if got := validColor(tc.in); got != tc.want {
			t.Errorf("validColor(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// harness wires a real server, registry and controller around a fake bulb, so
// these tests exercise the whole path without hardware.
type harness struct {
	ctrl *Controller
	reg  *registry.Registry
	bus  *events.Bus
	fb   *fakebulb.Bulb
}

func newHarness(t *testing.T, opts fakebulb.Options) *harness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	srv := server.New(reg, bus, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, ln) }()

	if opts.Version == "" && !opts.Malformed {
		opts.Version = "aigo_light_cct_v4.0.0"
	}
	fb, err := fakebulb.Dial(ln.Addr().String(), opts)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		fb.Close()
		cancel()
		<-done
	})
	h := &harness{ctrl: New(lights.New(reg, bus), reg), reg: reg, bus: bus, fb: fb}
	waitFor(t, "the bulb to register", func() bool {
		b, ok := reg.View(fb.DeviceID())
		return ok && b.Capabilities.Known
	})
	return h
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *harness) run(line string) Result {
	return h.ctrl.Execute(context.Background(), line)
}

func TestAdoptionGate(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	id := h.fb.DeviceID()

	// A discovered bulb answers list and info but refuses everything else.
	for _, line := range []string{"on " + id, "off " + id, "bri " + id + " 50", "temp " + id + " 50"} {
		res := h.run(line)
		if res.Kind != ResultError || !strings.Contains(res.Text, "not adopted yet") {
			t.Errorf("%q gave %q, want the adoption error", line, res.Text)
		}
	}
	if res := h.run("info " + id); res.Kind != ResultInfo {
		t.Errorf("info on a discovered bulb should work, got %q", res.Text)
	}
	if res := h.run("list"); res.Kind != ResultInfo || !strings.Contains(res.Text, id) {
		t.Errorf("list should show a discovered bulb, got %q", res.Text)
	}

	// Naming adopts it.
	res := h.run("name " + id + " kitchen")
	if res.Kind != ResultOK || !strings.Contains(res.Text, "adopted") {
		t.Fatalf("adoption failed: %q", res.Text)
	}
	if b, _ := h.reg.View(id); b.Status != bulb.Connected {
		t.Errorf("status after adoption = %v, want connected", b.Status)
	}

	// A second name is a rename, not another adoption.
	res = h.run("name kitchen kitchen-2")
	if res.Kind != ResultOK || !strings.Contains(res.Text, "renamed from kitchen") {
		t.Errorf("rename gave %q", res.Text)
	}
}

func TestCommandRoundTripAndValidation(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	if res := h.run("name " + h.fb.DeviceID() + " kitchen"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}

	tests := []struct {
		name     string
		line     string
		wantKind ResultKind
		contains string
		settled  func(bulb.LightState) bool
	}{
		{
			name: "on", line: "on kitchen", wantKind: ResultOK, contains: "kitchen: on",
			settled: func(s bulb.LightState) bool { return s.Power },
		},
		{
			name: "brightness", line: "bri kitchen 80", wantKind: ResultOK, contains: "brightness 80",
			settled: func(s bulb.LightState) bool { return s.Brightness == 80 },
		},
		{
			name: "temperature", line: "temp kitchen 50", wantKind: ResultOK, contains: "temperature 50",
			settled: func(s bulb.LightState) bool { return s.ColorTemp == 50 },
		},
		{
			name: "off", line: "off kitchen", wantKind: ResultOK, contains: "kitchen: off",
			settled: func(s bulb.LightState) bool { return !s.Power },
		},
		{name: "brightness out of range", line: "bri kitchen 150", wantKind: ResultError, contains: "brightness must be 0-100, got 150"},
		{name: "negative brightness", line: "bri kitchen -1", wantKind: ResultError, contains: "brightness must be 0-100"},
		{name: "temperature out of range", line: "temp kitchen 500", wantKind: ResultError, contains: "colour temperature must be 0-100"},
		{name: "unknown bulb", line: "on nosuchbulb", wantKind: ResultError, contains: `unknown bulb "nosuchbulb"`},
		{name: "unknown command", line: "dim kitchen", wantKind: ResultError, contains: `unknown command "dim"`},
		{name: "malformed colour", line: "color kitchen chartreuse", wantKind: ResultError, contains: "colour must be #RRGGBB or a name"},
		{name: "colour on a white-only bulb", line: "color kitchen red", wantKind: ResultError, contains: "does not support colour"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := h.run(tc.line)
			if res.Kind != tc.wantKind {
				t.Fatalf("kind = %v (%q), want %v", res.Kind, res.Text, tc.wantKind)
			}
			if !strings.Contains(res.Text, tc.contains) {
				t.Fatalf("text = %q, want it to contain %q", res.Text, tc.contains)
			}
			if tc.settled != nil {
				waitFor(t, "the bulb to report the change", func() bool {
					b, _ := h.reg.View(h.fb.DeviceID())
					return tc.settled(b.State)
				})
			}
		})
	}
}

// A refused command must not touch any bulb.
func TestRefusedCommandChangesNothing(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	if res := h.run("name " + h.fb.DeviceID() + " kitchen"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	if res := h.run("bri kitchen 60"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	waitFor(t, "brightness 60", func() bool {
		b, _ := h.reg.View(h.fb.DeviceID())
		return b.State.Brightness == 60
	})

	for _, line := range []string{"bri kitchen 500", "dim kitchen", "color kitchen red", "on nosuchbulb"} {
		if res := h.run(line); res.Kind != ResultError {
			t.Errorf("%q should have been refused", line)
		}
	}
	b, _ := h.reg.View(h.fb.DeviceID())
	if b.State.Brightness != 60 {
		t.Errorf("a refused command changed the bulb: brightness = %d", b.State.Brightness)
	}
}

func TestDisconnectedBulbFailsFast(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	id := h.fb.DeviceID()
	if res := h.run("name " + id + " kitchen"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	h.fb.Close()
	waitFor(t, "the disconnect", func() bool {
		b, _ := h.reg.View(id)
		return b.Status == bulb.Disconnected
	})

	start := time.Now()
	res := h.run("on kitchen")
	if res.Kind != ResultError || !strings.Contains(res.Text, "not connected") {
		t.Fatalf("res = %q, want a not-connected error", res.Text)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s; a disconnected bulb must fail immediately, not hang", elapsed)
	}
}

// A bulb that has not confirmed yet is reported as outstanding, not as failed.
// Calling it a failure was actively misleading: the operator watched the same
// command visibly succeed seconds after being told it had not worked.
func TestUnconfirmedCommandIsReportedHonestly(t *testing.T) {
	h := newHarness(t, fakebulb.Options{DeviceName: "silent", Silent: true})
	if res := h.run("name silent kitchen"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	waitFor(t, "the initial state report", func() bool {
		b, _ := h.reg.View("silent")
		return b.State.Brightness == 30
	})
	h.ctrl.SetTimeout(150 * time.Millisecond)

	res := h.run("bri kitchen 77")
	if res.Kind != ResultInfo {
		t.Fatalf("kind = %v (%q), want info: the command was delivered, only the confirmation is missing",
			res.Kind, res.Text)
	}
	if !strings.Contains(res.Text, "not confirmed yet") {
		t.Errorf("text = %q, should say the confirmation is outstanding", res.Text)
	}
	if strings.Contains(strings.ToLower(res.Text), "fail") {
		t.Errorf("text = %q, must not claim failure", res.Text)
	}
}

// An unconfirmed command must not raise a failure event either, or the feed
// would contradict the prompt.
func TestUnconfirmedCommandRaisesNoFailureEvent(t *testing.T) {
	h := newHarness(t, fakebulb.Options{DeviceName: "silent2", Silent: true})
	if res := h.run("name silent2 kitchen"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	waitFor(t, "the initial state report", func() bool {
		b, _ := h.reg.View("silent2")
		return b.State.Brightness == 30
	})
	sub := h.bus.Subscribe(64)
	defer sub.Close()
	h.ctrl.SetTimeout(150 * time.Millisecond)

	h.run("bri kitchen 77")
	time.Sleep(100 * time.Millisecond)
	for len(sub.Events()) > 0 {
		if e := <-sub.Events(); e.Kind == events.CommandResult {
			t.Errorf("an unconfirmed command published a failure event: %q", e.Detail)
		}
	}
}

// A bulb that is actually gone is still a hard error — "unconfirmed" applies
// only to commands we managed to deliver.
func TestDisconnectedBulbIsStillAnError(t *testing.T) {
	h := newHarness(t, fakebulb.Options{DeviceName: "gone"})
	if res := h.run("name gone kitchen"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	h.fb.Close()
	waitFor(t, "the disconnect", func() bool {
		b, _ := h.reg.View("gone")
		return b.Status == bulb.Disconnected
	})
	res := h.run("bri kitchen 50")
	if res.Kind != ResultError || !strings.Contains(res.Text, "not connected") {
		t.Fatalf("res = %q, want a not-connected error", res.Text)
	}
}

func TestNameCollisionRefused(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	id := h.fb.DeviceID()
	if res := h.run("name " + id + " kitchen"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	// A second bulb cannot take the same name.
	h.reg.Upsert("other", "addr", bulb.Capabilities{Known: true}, time.Now())
	res := h.run("name other kitchen")
	if res.Kind != ResultError || !strings.Contains(res.Text, "already used by") {
		t.Errorf("res = %q, want a name-collision error", res.Text)
	}
}

func TestAmbiguousTargetListsCandidates(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	if res := h.run("name " + h.fb.DeviceID() + " kitchen"); res.Kind != ResultOK {
		t.Fatal(res.Text)
	}
	h.reg.Upsert("kbbb", "addr", bulb.Capabilities{Known: true}, time.Now())
	if _, err := h.reg.Rename("kbbb", "kids-room"); err != nil {
		t.Fatal(err)
	}
	res := h.run("on k")
	if res.Kind != ResultError || !strings.Contains(res.Text, "ambiguous") {
		t.Fatalf("res = %q, want an ambiguity error", res.Text)
	}
	if !strings.Contains(res.Text, "kitchen") || !strings.Contains(res.Text, "kids-room") {
		t.Errorf("the error should list both candidates: %q", res.Text)
	}
}

func TestHelpCoversEveryVerb(t *testing.T) {
	help := Help("")
	for _, verb := range Verbs {
		if !strings.Contains(help, verb) {
			t.Errorf("help does not mention %q", verb)
		}
	}
	if !strings.Contains(help, "adopts it") {
		t.Error("help must explain adoption; it is the first thing a new operator hits")
	}
	if got := Help("bri"); !strings.Contains(got, "brightness") {
		t.Errorf("help bri = %q", got)
	}
	if got := Help("nope"); !strings.Contains(got, "no such command") {
		t.Errorf("help for an unknown verb = %q", got)
	}
}

func TestResultLineShapes(t *testing.T) {
	tests := []struct {
		res  Result
		want string
	}{
		{Result{Kind: ResultOK, Text: "kitchen: on"}, "ok      kitchen: on"},
		{Result{Kind: ResultError, Text: "boom"}, "error   boom"},
		{Result{Kind: ResultInfo, Text: "hello"}, "info    hello"},
	}
	for _, tc := range tests {
		if got := tc.res.Line(); got != tc.want {
			t.Errorf("Line() = %q, want %q", got, tc.want)
		}
	}
}
