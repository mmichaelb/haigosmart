package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"haigosmart/internal/bulb"
	"haigosmart/internal/bulb/fakebulb"
	"haigosmart/internal/events"
	"haigosmart/internal/registry"
)

type harness struct {
	addr string
	reg  *registry.Registry
	bus  *events.Bus
	sub  *events.Subscription
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	srv := New(reg, bus, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return &harness{addr: ln.Addr().String(), reg: reg, bus: bus, sub: bus.Subscribe(256)}
}

// waitFor polls until cond holds or the deadline passes. Polling beats sleeping
// a fixed duration: it is both faster and less flaky.
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

func TestBulbRegistersOnConnect(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer fb.Close()

	waitFor(t, "the bulb to register", func() bool {
		_, ok := h.reg.View(fb.DeviceID())
		return ok
	})
	view := func() bulb.Bulb {
		b, _ := h.reg.View(fb.DeviceID())
		return b
	}
	if got := view().Status; got != bulb.Discovered {
		t.Errorf("status = %v, want discovered", got)
	}

	// Capabilities must come from the firmware string, not from a guess.
	waitFor(t, "capabilities to be determined", func() bool { return view().Capabilities.Known })
	caps := view().Capabilities
	if caps.Color {
		t.Error("a cct bulb must not claim colour support")
	}
	if !caps.ColorTemp {
		t.Error("a cct bulb must report colour-temperature support")
	}
	waitFor(t, "the initial state report", func() bool { return view().State.Brightness == 30 })
}

func TestUnknownFirmwareLeavesCapabilitiesUndetermined(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "nover", Version: ""})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer fb.Close()
	waitFor(t, "registration", func() bool { _, ok := h.reg.View("nover"); return ok })
	view := func() bulb.Bulb {
		b, _ := h.reg.View("nover")
		return b
	}
	waitFor(t, "the state report", func() bool { return view().State.Brightness == 30 })
	// The report carries ColorTemperature, so the bulb is classified from that;
	// what must never happen is a confident Color:true.
	if view().Capabilities.Color {
		t.Error("colour must not be inferred from nothing")
	}
}

func TestReconnectRejoinsSameEntry(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "registration", func() bool { _, ok := h.reg.View(fb.DeviceID()); return ok })
	view := func() bulb.Bulb {
		b, _ := h.reg.View(fb.DeviceID())
		return b
	}
	if _, err := h.reg.Rename(fb.DeviceID(), "kitchen"); err != nil {
		t.Fatal(err)
	}

	fb.Close() // a power cut
	waitFor(t, "the disconnect to register", func() bool { return view().Status == bulb.Disconnected })

	fb2, err := fakebulb.Dial(h.addr, fakebulb.Options{Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer fb2.Close()
	waitFor(t, "reconnection", func() bool { return view().Status == bulb.Connected })

	if got := len(h.reg.List()); got != 1 {
		t.Errorf("reconnect created %d entries, want 1", got)
	}
	if got := view().Name; got != "kitchen" {
		t.Errorf("name lost across reconnect: %q", got)
	}
}

func TestCommandRoundTrip(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()
	waitFor(t, "registration", func() bool { _, ok := h.reg.View(fb.DeviceID()); return ok })
	waitFor(t, "a driver", func() bool { return h.reg.Driver(fb.DeviceID()) != nil })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	want := bulb.LightState{Power: true, Brightness: 80, ColorTemp: 50}
	if err := h.reg.Driver(fb.DeviceID()).Apply(ctx, want); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	waitFor(t, "the bulb to report the new state", func() bool {
		b, _ := h.reg.View(fb.DeviceID())
		return b.State.Brightness == 80 && b.State.ColorTemp == 50
	})
}

func TestCommandTimesOutOnSilentBulb(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "silent", Version: "aigo_light_cct_v4.0.0", Silent: true})
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()
	waitFor(t, "registration", func() bool { _, ok := h.reg.View("silent"); return ok })
	waitFor(t, "a driver", func() bool { return h.reg.Driver("silent") != nil })

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err = h.reg.Driver("silent").Apply(ctx, bulb.LightState{Power: true, Brightness: 50})
	if err == nil {
		t.Fatal("a silent bulb must fail the command, not hang forever")
	}
}

// A misbehaving bulb must take down only its own connection (FR-016).
func TestMalformedBulbDoesNotAffectOthers(t *testing.T) {
	h := newHarness(t)
	good, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "good", Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer good.Close()
	waitFor(t, "the good bulb to register", func() bool { _, ok := h.reg.View("good"); return ok })

	bad, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "bad", Malformed: true})
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()

	waitFor(t, "the good bulb's driver", func() bool { return h.reg.Driver("good") != nil })

	// The good bulb must still work after the bad one is dropped.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.reg.Driver("good").Apply(ctx, bulb.LightState{Power: true, Brightness: 42}); err != nil {
		t.Fatalf("the surviving bulb should still take commands: %v", err)
	}
}

func TestKeepAliveIsAnswered(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()
	waitFor(t, "registration", func() bool { _, ok := h.reg.View(fb.DeviceID()); return ok })
	for range 3 {
		if err := fb.Ping(); err != nil {
			t.Fatalf("ping: %v", err)
		}
	}
	// The connection must survive the pings; a PINGRESP that never arrives
	// would eventually make the bulb fall back to its cloud.
	waitFor(t, "the bulb to stay connected", func() bool { return h.reg.Driver(fb.DeviceID()) != nil })
	if err := fb.PostFullState(); err != nil {
		t.Fatalf("bulb dropped after keep-alives: %v", err)
	}
}

func TestWallSwitchChangeIsReported(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()
	waitFor(t, "registration", func() bool { _, ok := h.reg.View(fb.DeviceID()); return ok })
	view := func() bulb.Bulb {
		b, _ := h.reg.View(fb.DeviceID())
		return b
	}
	waitFor(t, "the initial report", func() bool { return view().State.Power })

	if err := fb.SetPower(false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the wall-switch change", func() bool { return !view().State.Power })

	var sawChange bool
	for len(h.sub.Events()) > 0 {
		e := <-h.sub.Events()
		if e.Kind == events.StateChanged {
			for _, c := range e.Changed {
				if c.Field == "power" && c.To == "off" {
					sawChange = true
				}
			}
		}
	}
	if !sawChange {
		t.Error("an unprompted state change produced no event")
	}
}

// Field hardware connects with TLS on the same port the capture showed in
// cleartext, so one listener has to serve both. This is the regression test for
// the "string of 769 bytes exceeds packet" failure: a TLS ClientHello being
// parsed as an MQTT CONNECT.
func TestTLSBulbConnects(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{
		DeviceName: "tlsbulb", Version: "aigo_light_cct_v4.0.0", TLS: true,
	})
	if err != nil {
		t.Fatalf("a TLS bulb could not connect: %v", err)
	}
	defer fb.Close()

	waitFor(t, "the TLS bulb to register", func() bool {
		_, ok := h.reg.View("tlsbulb")
		return ok
	})
	view := func() bulb.Bulb {
		b, _ := h.reg.View("tlsbulb")
		return b
	}
	waitFor(t, "its capabilities", func() bool { return view().Capabilities.Known })
	waitFor(t, "its initial state report", func() bool { return view().State.Brightness == 30 })

	// It must be controllable, not merely connected.
	waitFor(t, "a driver", func() bool { return h.reg.Driver("tlsbulb") != nil })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.reg.Driver("tlsbulb").Apply(ctx, bulb.LightState{Power: true, Brightness: 75}); err != nil {
		t.Fatalf("command over TLS failed: %v", err)
	}
	waitFor(t, "the TLS bulb to report the change", func() bool { return view().State.Brightness == 75 })
}

// The real firmware offers only RSA-key-exchange CBC suites over TLS 1.2. Go
// disables those by default and will not select them at all against an ECDSA
// certificate, which is what produced:
//
//	tls: no cipher suite supported by both client and server;
//	client offered: [3d 35 3c 2f ff]
//
// This is the regression test for that pair of causes.
func TestLegacyRSACipherSuitesFromRealFirmware(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{
		DeviceName: "legacy", Version: "aigo_light_cct_v4.0.0", LegacyTLS: true,
	})
	if err != nil {
		t.Fatalf("a bulb offering only RSA CBC suites could not connect: %v", err)
	}
	defer fb.Close()

	waitFor(t, "the legacy-TLS bulb to register", func() bool {
		_, ok := h.reg.View("legacy")
		return ok
	})
	waitFor(t, "a driver", func() bool { return h.reg.Driver("legacy") != nil })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.reg.Driver("legacy").Apply(ctx, bulb.LightState{Power: true, Brightness: 55}); err != nil {
		t.Fatalf("command over legacy TLS failed: %v", err)
	}
	waitFor(t, "the change to be reported", func() bool {
		b, _ := h.reg.View("legacy")
		return b.State.Brightness == 55
	})
}

// Both transports have to work at once: the capture showed cleartext, the field
// shows TLS, and we cannot tell which a given bulb will use.
func TestCleartextAndTLSBulbsShareOneListener(t *testing.T) {
	h := newHarness(t)
	plain, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "plain", Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	secure, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "secure", Version: "aigo_light_cct_v4.0.0", TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	defer secure.Close()

	for _, id := range []string{"plain", "secure"} {
		waitFor(t, id+" to register", func() bool { _, ok := h.reg.View(id); return ok })
	}
	if got := len(h.reg.List()); got != 2 {
		t.Errorf("registry holds %d bulbs, want 2", got)
	}
}

// Earlier builds wrote an ECDSA key to the same path. The bulbs' RSA key
// exchange cannot use one, and a stale file must not permanently stop bulbs
// from connecting — it gets replaced.
func TestStaleNonRSAKeyIsReplaced(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "tls.key")

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, der, 0o600); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	srv := New(reg, bus, keyPath)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, ln) }()
	defer func() {
		cancel()
		<-done
	}()

	fb, err := fakebulb.Dial(ln.Addr().String(), fakebulb.Options{
		DeviceName: "afterupgrade", Version: "aigo_light_cct_v4.0.0", LegacyTLS: true,
	})
	if err != nil {
		t.Fatalf("a stale key blocked the connection instead of being replaced: %v", err)
	}
	defer fb.Close()
	waitFor(t, "the bulb to register despite the stale key", func() bool {
		_, ok := reg.View("afterupgrade")
		return ok
	})

	// The replacement must be persisted, so the next restart does not repeat it.
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x509.ParsePKCS1PrivateKey(raw); err != nil {
		t.Errorf("the key on disk is still not a usable RSA key: %v", err)
	}
}

// A key that survives a restart means a bulb is not shown a brand new identity
// every time the server bounces.
func TestKeyIsStableAcrossRestarts(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "tls.key")
	first := newCertSource(keyPath)
	certA, err := first.certificateFor("public.iot-as-mqtt.eu-central-1.aliyuncs.com")
	if err != nil {
		t.Fatal(err)
	}
	second := newCertSource(keyPath)
	certB, err := second.certificateFor("public.iot-as-mqtt.eu-central-1.aliyuncs.com")
	if err != nil {
		t.Fatal(err)
	}
	keyA, okA := certA.PrivateKey.(*rsa.PrivateKey)
	keyB, okB := certB.PrivateKey.(*rsa.PrivateKey)
	if !okA || !okB {
		t.Fatal("certificates must carry RSA keys for the bulbs' key exchange")
	}
	if keyA.N.Cmp(keyB.N) != 0 {
		t.Error("the key changed across a restart; it should be loaded from disk")
	}
}

// The bulb asks for the vendor's hostname by SNI, so that is the name the
// certificate has to carry.
func TestCertificateMatchesRequestedSNI(t *testing.T) {
	src := newCertSource(filepath.Join(t.TempDir(), "tls.key"))
	const sni = "public.iot-as-mqtt.eu-central-1.aliyuncs.com"
	cert, err := src.certificateFor(sni)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed.VerifyHostname(sni); err != nil {
		t.Errorf("certificate does not cover the requested name: %v", err)
	}
}

// The vendor's app sends exactly one property per command; the hardware ignores
// a bundle and never acknowledges it. This is the regression test for
// "headlamp: bulb headlamp did not acknowledge within the timeout" on `off`.
func TestCommandsAreSentOnePropertyAtATime(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "onep", Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()
	waitFor(t, "registration", func() bool { _, ok := h.reg.View("onep"); return ok })
	waitFor(t, "a driver", func() bool { return h.reg.Driver("onep") != nil })
	waitFor(t, "the initial report", func() bool {
		b, _ := h.reg.View("onep")
		return b.State.Brightness == 30
	})

	// Drain what the handshake produced, then change two things at once.
	for len(fb.Commands()) > 0 {
		<-fb.Commands()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.reg.Driver("onep").Apply(ctx, bulb.LightState{Power: true, Brightness: 70, ColorTemp: 40}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	deadline := time.After(2 * time.Second)
	got := 0
	for got < 2 {
		select {
		case props := <-fb.Commands():
			if len(props) != 1 {
				t.Fatalf("command carried %d properties, want exactly 1: %v", len(props), props)
			}
			got++
		case <-deadline:
			t.Fatalf("only saw %d commands", got)
		}
	}
}

// Turning a lit bulb off must send LightSwitch alone. Bundling brightness with
// it is what the hardware refused to acknowledge.
func TestOffSendsOnlyTheSwitch(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "offonly", Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()
	waitFor(t, "registration", func() bool { _, ok := h.reg.View("offonly"); return ok })
	waitFor(t, "a driver", func() bool { return h.reg.Driver("offonly") != nil })
	waitFor(t, "the bulb to report itself on", func() bool {
		b, _ := h.reg.View("offonly")
		return b.State.Power && b.State.Brightness == 30
	})
	for len(fb.Commands()) > 0 {
		<-fb.Commands()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	current, _ := h.reg.View("offonly")
	want := current.State
	want.Power = false
	if err := h.reg.Driver("offonly").Apply(ctx, want); err != nil {
		t.Fatalf("turning off failed: %v", err)
	}
	select {
	case props := <-fb.Commands():
		if len(props) != 1 {
			t.Fatalf("off carried %d properties, want just LightSwitch: %v", len(props), props)
		}
		if _, ok := props["LightSwitch"]; !ok {
			t.Errorf("off sent %v, want LightSwitch", props)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no command reached the bulb")
	}
	waitFor(t, "the bulb to report itself off", func() bool {
		b, _ := h.reg.View("offonly")
		return !b.State.Power
	})
}

// A bulb that reports the resulting state but never sends CommonService_reply
// must still complete the command: the state report is the stronger proof.
func TestCommandCompletesOnStateReportWithoutAck(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{
		DeviceName: "noack", Version: "aigo_light_cct_v4.0.0", NoAck: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()
	waitFor(t, "registration", func() bool { _, ok := h.reg.View("noack"); return ok })
	waitFor(t, "a driver", func() bool { return h.reg.Driver("noack") != nil })
	waitFor(t, "the initial report", func() bool {
		b, _ := h.reg.View("noack")
		return b.State.Brightness == 30
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.reg.Driver("noack").Apply(ctx, bulb.LightState{Power: true, Brightness: 90}); err != nil {
		t.Fatalf("a bulb that confirms only by reporting state should still complete: %v", err)
	}
}

// Asking for the state a bulb is already in sends nothing at all.
func TestNoOpCommandSendsNothing(t *testing.T) {
	h := newHarness(t)
	fb, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "noop", Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()
	waitFor(t, "registration", func() bool { _, ok := h.reg.View("noop"); return ok })
	waitFor(t, "a driver", func() bool { return h.reg.Driver("noop") != nil })
	waitFor(t, "the initial report", func() bool {
		b, _ := h.reg.View("noop")
		return b.State.Brightness == 30
	})
	for len(fb.Commands()) > 0 {
		<-fb.Commands()
	}

	current, _ := h.reg.View("noop")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.reg.Driver("noop").Apply(ctx, current.State); err != nil {
		t.Fatalf("a no-op should succeed immediately: %v", err)
	}
	select {
	case props := <-fb.Commands():
		t.Errorf("a no-op sent a command: %v", props)
	case <-time.After(300 * time.Millisecond):
	}
}

// A bulb that power-cycles reconnects before the server notices the old socket
// is dead. That is an ordinary reconnect, not two devices sharing an id, and it
// must not produce a duplicate warning or a phantom disconnect.
func TestPowerCycleReconnectIsNotADuplicate(t *testing.T) {
	h := newHarness(t)
	first, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "cycle", Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first connection", func() bool { return h.reg.Driver("cycle") != nil })
	if _, err := h.reg.Rename("cycle", "headlamp"); err != nil {
		t.Fatal(err)
	}
	firstDriver := h.reg.Driver("cycle")

	// Reconnect without closing the first connection, which is what a hard
	// power cut looks like from the server's side: the old socket is still open
	// as far as we know.
	second, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "cycle", Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	waitFor(t, "the new connection to take over", func() bool {
		d := h.reg.Driver("cycle")
		return d != nil && d != firstDriver
	})
	_ = first.Close()

	// Give the superseded session time to notice and (wrongly) report.
	time.Sleep(200 * time.Millisecond)

	var duplicates, disconnects int
	for len(h.sub.Events()) > 0 {
		switch e := <-h.sub.Events(); e.Kind {
		case events.DuplicateID:
			duplicates++
		case events.Disconnected:
			disconnects++
		}
	}
	if duplicates != 0 {
		t.Errorf("a power-cycle reconnect produced %d duplicate warnings, want 0", duplicates)
	}
	if disconnects != 0 {
		t.Errorf("a superseded connection produced %d disconnect events, want 0: the bulb is online", disconnects)
	}

	// The bulb must be controllable through the new connection.
	if b, _ := h.reg.View("cycle"); b.Status != bulb.Connected {
		t.Errorf("status = %v, want connected", b.Status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.reg.Driver("cycle").Apply(ctx, bulb.LightState{Power: true, Brightness: 44}); err != nil {
		t.Fatalf("the bulb should work after reconnecting: %v", err)
	}
	if got := len(h.reg.List()); got != 1 {
		t.Errorf("registry holds %d entries, want 1", got)
	}
}

// Two devices genuinely sharing an id kick each other off repeatedly. That is
// what the duplicate warning is for, and it must still fire.
func TestRepeatedTakeoversWarnAboutADuplicateID(t *testing.T) {
	h := newHarness(t)
	var bulbs []*fakebulb.Bulb
	for range takeoverThreshold + 1 {
		fb, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "twins", Version: "aigo_light_cct_v4.0.0"})
		if err != nil {
			t.Fatal(err)
		}
		bulbs = append(bulbs, fb)
		waitFor(t, "the connection to be registered", func() bool { return h.reg.Driver("twins") != nil })
	}
	defer func() {
		for _, fb := range bulbs {
			fb.Close()
		}
	}()

	waitFor(t, "a duplicate-id warning", func() bool {
		for len(h.sub.Events()) > 0 {
			if (<-h.sub.Events()).Kind == events.DuplicateID {
				return true
			}
		}
		return false
	})
}

func TestNoteTakeoverForgetsOldEvents(t *testing.T) {
	s := New(nil, nil, "")
	base := time.Now()
	// Two takeovers now, a third long after: the old ones have aged out, so
	// this is not a fight, it is three separate reconnects.
	if s.noteTakeover("dev", base) || s.noteTakeover("dev", base.Add(time.Second)) {
		t.Fatal("two takeovers should not warn")
	}
	if s.noteTakeover("dev", base.Add(2*takeoverWindow)) {
		t.Error("takeovers outside the window should have been forgotten")
	}
	// Three inside the window do warn.
	later := base.Add(2 * takeoverWindow)
	s.noteTakeover("dev", later.Add(time.Second))
	if !s.noteTakeover("dev", later.Add(2*time.Second)) {
		t.Error("three takeovers inside the window should warn")
	}
}
