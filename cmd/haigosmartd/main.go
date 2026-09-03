// Command haigosmartd is a local replacement for the Aigo cloud. Bulbs on the
// LAN connect to it instead of the vendor's servers, and an operator drives
// them from a terminal interface.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mmichaelb/haigosmart/internal/config"
	"github.com/mmichaelb/haigosmart/internal/control"
	"github.com/mmichaelb/haigosmart/internal/events"
	"github.com/mmichaelb/haigosmart/internal/hass"
	"github.com/mmichaelb/haigosmart/internal/lights"
	"github.com/mmichaelb/haigosmart/internal/logging"
	"github.com/mmichaelb/haigosmart/internal/mqtt"
	"github.com/mmichaelb/haigosmart/internal/registry"
	"github.com/mmichaelb/haigosmart/internal/server"
	"github.com/mmichaelb/haigosmart/internal/tui"
)

// version is the release this binary was built from. GoReleaser stamps it at
// link time; an ordinary `go build` leaves it as "dev", which is the honest
// answer rather than a fabricated number.
var version = "dev"

func main() {
	// Answered before anything else, deliberately. "Which build is this?" is the
	// first question asked about a deployment that will not start, and it must be
	// answerable while it is not starting — so this must not sit behind loading
	// or validating the configuration.
	for _, arg := range os.Args[1:] {
		if arg == "-version" || arg == "--version" {
			fmt.Println("haigosmartd", version)
			return
		}
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "haigosmartd:", err)
		os.Exit(1)
	}
}

func run() error {
	start := time.Now()

	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		return err
	}
	// Everything is checked before a socket opens, so a refusal costs nothing to
	// unwind and an operator learns about it immediately rather than at the
	// first connection.
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Go's runtime raises SIGPIPE when a write to fd 1 or 2 hits a broken pipe,
	// and the default disposition kills the process: status 141, nothing on
	// stderr, no explanation for whoever finds the dead container. Registering
	// for the signal makes those writes return EPIPE instead, so the record
	// stream's own failure path reports what happened and exits 1.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)

	logFile, logger, err := newLogger(cfg.LogPath, cfg.Headless, cfg.Verbose, start)
	if err != nil {
		return err
	}
	if logFile != nil {
		defer logFile.Close()
	}

	logger.Info("starting", "version", version, "config", cfg)
	for _, name := range cfg.Overrides {
		logger.Info("setting overridden on the command line", "setting", name)
	}

	store := registry.NewStore(cfg.RegistryPath, 2*time.Second)
	// A registry that cannot be written is a degradation, not a failure: the
	// file is a cache, and the configured lamp set survives without it. Report
	// the first failure loudly and the rest quietly, because a read-only mount
	// makes every save fail forever.
	var saveFailed sync.Once
	store.OnError = func(err error) {
		saveFailed.Do(func() {
			logger.Warn("saving the registry failed", "error", err, "path", cfg.RegistryPath)
		})
		logger.Debug("saving the registry failed", "error", err, "path", cfg.RegistryPath)
	}
	reg, err := store.Load()
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("saving the registry on shutdown failed", "error", err, "path", cfg.RegistryPath)
		}
	}()

	bus := events.NewBus(logger)
	srv := server.New(reg, bus, filepath.Join(filepath.Dir(cfg.RegistryPath), "tls.key"))

	// The configured set is authoritative. Declaring before the listener opens
	// means a configured lamp is present, named and unavailable from the first
	// second, rather than materialising whenever it happens to connect.
	if err := declareLamps(reg, cfg, logger); err != nil {
		return err
	}
	if cfg.Headless {
		configured := make(map[string]bool, len(cfg.Lamps))
		for _, l := range cfg.Lamps {
			configured[l.DeviceID] = true
		}
		srv.Admit = func(deviceID string) bool { return configured[deviceID] }
	}
	svc := lights.New(reg, bus)
	svc.SetTimeout(cfg.CommandTimeout)
	ctrl := control.New(svc, reg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// NotifyContext cancels but does not say which signal did it, and the
	// shutdown record is more useful when it does. Both registrations receive
	// the same signal.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.ListenAndServe(ctx, cfg.Listen) }()

	// The Home Assistant bridge is optional and must never be able to stop the
	// server starting: a broker that is down is a broker problem, and the lamps
	// keep working from the terminal regardless.
	if cfg.MQTTBroker != "" {
		hassCfg := hass.Config{
			DiscoveryPrefix: cfg.DiscoveryPrefix,
			Prefix:          cfg.MQTTPrefix,
			MinKelvin:       cfg.MinKelvin,
			MaxKelvin:       cfg.MaxKelvin,
		}
		var bridge *hass.Bridge
		client := mqtt.New(mqtt.Options{
			Broker:      cfg.MQTTBroker,
			ClientID:    cfg.MQTTClientID,
			Username:    cfg.MQTTUsername,
			Password:    cfg.MQTTPassword,
			WillTopic:   hassCfg.StatusTopic(),
			WillPayload: []byte(hass.Offline),
			WillRetain:  true,
			Logger:      logger,
			OnConnect:   func() { bridge.OnConnect() },
		})
		bridge = hass.New(hassCfg, svc, client, logger)

		go func() { _ = client.Run(ctx) }()
		go func() { _ = bridge.Run(ctx) }()
		logger.Info("home assistant integration enabled", "broker", cfg.MQTTBroker,
			"kelvin_range", fmt.Sprintf("%d-%d", cfg.MinKelvin, cfg.MaxKelvin))
	}

	if cfg.Headless {
		logger.Info("listening for bulbs", "addr", cfg.Listen, "registry", cfg.RegistryPath)
		select {
		case err := <-serverErr:
			return err
		case <-ctx.Done():
			logger.Info("shutting down", "signal", signalName(sigs))
			// Persisting on the way out is a code path rather than an accident
			// of the deferred Close's timing, so a debounced save still in
			// flight cannot be lost to the process exiting first.
			if err := store.Flush(); err != nil {
				logger.Error("saving the registry on shutdown failed", "error", err, "path", cfg.RegistryPath)
			}
			return <-serverErr
		}
	}

	sub := bus.Subscribe(1024)
	defer sub.Close()
	model := tui.New(ctrl, reg, sub, cancel)
	program := tea.NewProgram(model)

	if _, err := program.Run(); err != nil {
		cancel()
		<-serverErr
		return fmt.Errorf("running the terminal interface: %w", err)
	}
	cancel()
	return <-serverErr
}

// newLogger sets up structured logging. Records are JSON lines in both modes,
// in the format fixed by specs/003-headless-deployment/contracts/log-records.md.
//
// Where they go differs. Headless mode writes to standard output, because that
// is a container's log stream and because there is no display to protect. With
// the terminal interface running they must not go to a stream at all — they
// would scribble over the display — so they go to a file, which is also what
// makes them useful after the fact.
func newLogger(path string, headless, verbose bool, start time.Time) (*os.File, *slog.Logger, error) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	if path == "" {
		if headless {
			// A failed write here stops the process: an unattended server whose
			// records reach nobody is not running.
			w := &logging.FatalWriter{W: os.Stdout, Stderr: os.Stderr, Exit: os.Exit}
			return nil, logging.New(w, level, start), nil
		}
		path = filepath.Join(os.TempDir(), "haigosmart.log")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file %s: %w. pass -log to choose another path", path, err)
	}
	return f, logging.New(f, level, start), nil
}

// signalName reports which signal arrived, or "unknown" when shutdown came from
// somewhere else — the terminal quitting, for instance.
func signalName(sigs <-chan os.Signal) string {
	select {
	case sig := <-sigs:
		return sig.String()
	default:
		return "unknown"
	}
}

// declareLamps applies the configured lamp set to the registry and reports what
// it found.
//
// Registry entries the configuration does not name are left on disk untouched —
// the interactive mode should still show everything ever adopted — but they are
// reported, because the failure this catches is otherwise silent: one lamp
// dropped from a manifest by a bad edit, and the only symptom is a room that
// stops responding.
func declareLamps(reg *registry.Registry, cfg config.Config, logger *slog.Logger) error {
	configured := make(map[string]bool, len(cfg.Lamps))
	for _, l := range cfg.Lamps {
		created, renamed, err := reg.Declare(l.DeviceID, l.Name)
		if err != nil {
			return fmt.Errorf("configured lamp %s=%s: %w", l.DeviceID, l.Name, err)
		}
		configured[l.DeviceID] = true
		logger.Info("lamp configured", "device", l.DeviceID, "name", l.Name,
			"created", created, "renamed", renamed)
	}
	if len(cfg.Lamps) == 0 {
		return nil
	}
	for _, b := range reg.List() {
		if !configured[b.DeviceID] {
			logger.Warn("registry lamp not configured", "device", b.DeviceID, "name", b.Name)
		}
	}
	return nil
}
