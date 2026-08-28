// Command haigosmartd is a local replacement for the Aigo cloud. Bulbs on the
// LAN connect to it instead of the vendor's servers, and an operator drives
// them from a terminal interface.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"haigosmart/internal/control"
	"haigosmart/internal/events"
	"haigosmart/internal/hass"
	"haigosmart/internal/lights"
	"haigosmart/internal/mqtt"
	"haigosmart/internal/registry"
	"haigosmart/internal/server"
	"haigosmart/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "haigosmartd:", err)
		os.Exit(1)
	}
}

func run() error {
	defaultRegistry, err := registry.DefaultPath()
	if err != nil {
		defaultRegistry = "registry.json"
	}
	var (
		addr        = flag.String("listen", server.DefaultAddr, "address to accept bulb connections on")
		registryArg = flag.String("registry", defaultRegistry, "path to the registry file")
		logPath     = flag.String("log", "", "write structured logs here (default: stderr in -headless, a temp file otherwise)")
		headless    = flag.Bool("headless", false, "run without the terminal interface")
		verbose     = flag.Bool("v", false, "debug logging, including per-frame protocol traces")
		cmdTimeout  = flag.Duration("command-timeout", control.CommandTimeout,
			"how long to wait for a bulb to confirm a command; bulbs that fade report only once the fade finishes")

		mqttBroker  = flag.String("mqtt-broker", "", "host:port of your MQTT broker; empty disables the Home Assistant integration")
		mqttUser    = flag.String("mqtt-username", "", "broker username, if the broker needs one")
		mqttPass    = flag.String("mqtt-password", "", "broker password, if the broker needs one")
		mqttClient  = flag.String("mqtt-client-id", "haigosmart", "client id presented to the broker")
		mqttPrefix  = flag.String("mqtt-prefix", "haigosmart", "base topic for state, availability and commands")
		discPrefix  = flag.String("mqtt-discovery-prefix", "homeassistant", "Home Assistant discovery prefix")
		ctMinKelvin = flag.Int("ct-min-kelvin", 2700, "Kelvin at the lamp's warmest setting")
		ctMaxKelvin = flag.Int("ct-max-kelvin", 6500, "Kelvin at the lamp's coolest setting")
	)
	flag.Parse()

	// An inverted Kelvin range would silently reverse every warmth value shown
	// in Home Assistant, so it is refused rather than accepted and worked around.
	if *ctMinKelvin >= *ctMaxKelvin {
		return fmt.Errorf("-ct-min-kelvin (%d) must be below -ct-max-kelvin (%d)", *ctMinKelvin, *ctMaxKelvin)
	}

	logFile, logger, err := newLogger(*logPath, *headless, *verbose)
	if err != nil {
		return err
	}
	if logFile != nil {
		defer logFile.Close()
	}

	store := registry.NewStore(*registryArg, 2*time.Second)
	reg, err := store.Load()
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("saving the registry on shutdown failed", "error", err, "path", *registryArg)
		}
	}()

	bus := events.NewBus(logger)
	srv := server.New(reg, bus, filepath.Join(filepath.Dir(*registryArg), "tls.key"))
	svc := lights.New(reg, bus)
	svc.SetTimeout(*cmdTimeout)
	ctrl := control.New(svc, reg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.ListenAndServe(ctx, *addr) }()

	// The Home Assistant bridge is optional and must never be able to stop the
	// server starting: a broker that is down is a broker problem, and the lamps
	// keep working from the terminal regardless.
	if *mqttBroker != "" {
		hassCfg := hass.Config{
			DiscoveryPrefix: *discPrefix,
			Prefix:          *mqttPrefix,
			MinKelvin:       *ctMinKelvin,
			MaxKelvin:       *ctMaxKelvin,
		}
		var bridge *hass.Bridge
		client := mqtt.New(mqtt.Options{
			Broker:      *mqttBroker,
			ClientID:    *mqttClient,
			Username:    *mqttUser,
			Password:    *mqttPass,
			WillTopic:   hassCfg.StatusTopic(),
			WillPayload: []byte(hass.Offline),
			WillRetain:  true,
			Logger:      logger,
			OnConnect:   func() { bridge.OnConnect() },
		})
		bridge = hass.New(hassCfg, svc, client, logger)

		go func() { _ = client.Run(ctx) }()
		go func() { _ = bridge.Run(ctx) }()
		logger.Info("home assistant integration enabled", "broker", *mqttBroker,
			"kelvin_range", fmt.Sprintf("%d-%d", *ctMinKelvin, *ctMaxKelvin))
	}

	if *headless {
		logger.Info("listening for bulbs", "addr", *addr, "registry", *registryArg)
		select {
		case err := <-serverErr:
			return err
		case <-ctx.Done():
			return <-serverErr
		}
	}

	sub := bus.Subscribe(1024)
	defer sub.Close()
	model := tui.New(ctrl, reg, sub, cancel)
	program := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := program.Run(); err != nil {
		cancel()
		<-serverErr
		return fmt.Errorf("running the terminal interface: %w", err)
	}
	cancel()
	return <-serverErr
}

// newLogger sets up structured logging. With the terminal interface running,
// logs must not go to stderr: they would scribble over the display. They go to
// a file instead, which is also what makes them useful after the fact.
func newLogger(path string, headless, verbose bool) (*os.File, *slog.Logger, error) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	if path == "" {
		if headless {
			return nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})), nil
		}
		path = filepathJoinTemp("haigosmart.log")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file %s: %w. pass -log to choose another path", path, err)
	}
	return f, slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level})), nil
}

func filepathJoinTemp(name string) string {
	return os.TempDir() + string(os.PathSeparator) + name
}
