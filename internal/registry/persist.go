package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
)

// FileVersion is the schema version of the registry file. An unknown version is
// a startup error, never a silent overwrite of the operator's data.
const FileVersion = 1

// file is the on-disk shape, per contracts/registry-file.md.
type file struct {
	Version int          `json:"version"`
	Bulbs   []storedBulb `json:"bulbs"`
}

type storedBulb struct {
	DeviceID     string            `json:"device_id"`
	Name         string            `json:"name"`
	FirstSeen    time.Time         `json:"first_seen"`
	LastSeen     time.Time         `json:"last_seen"`
	Capabilities bulb.Capabilities `json:"capabilities"`
	Firmware     string            `json:"firmware,omitempty"`
	State        bulb.LightState   `json:"state"`
}

// Store persists a registry to a JSON file, coalescing rapid changes into one
// write.
type Store struct {
	path string

	// OnError reports a failed background save. Background failures were
	// discarded before this hook existed, which was harmless while a person was
	// watching the terminal and the shutdown save would report the problem. A
	// read-only registry directory makes every save fail forever, so the failure
	// has to be reportable without a person present. Nil means discard; set it
	// before the registry is handed to anything that mutates it.
	OnError func(error)

	mu      sync.Mutex
	timer   *time.Timer
	pending bool
	reg     *Registry
	delay   time.Duration
}

// DefaultPath is the registry location when none is given.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config directory: %w", err)
	}
	return filepath.Join(dir, "haigosmart", "registry.json"), nil
}

// NewStore returns a store writing to path. Saves are debounced by delay so a
// burst of state reports produces a single file write.
func NewStore(path string, delay time.Duration) *Store {
	return &Store{path: path, delay: delay}
}

// Load reads the registry file and returns a populated registry. A missing file
// is a first run, not an error. A corrupt or unknown-version file is an error
// and the file is left untouched so the operator can inspect it.
func (s *Store) Load() (*Registry, error) {
	reg := New(s.schedule)
	s.mu.Lock()
	s.reg = reg
	s.mu.Unlock()

	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return reg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading registry %s: %w", s.path, err)
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("registry %s is corrupt: %w. the file was left untouched; inspect or restore it, or move it aside to start fresh", s.path, err)
	}
	if f.Version != FileVersion {
		return nil, fmt.Errorf("registry %s has version %d, expected %d. this build cannot read it; upgrade haigosmart or move the file aside", s.path, f.Version, FileVersion)
	}
	seen := make(map[string]bool, len(f.Bulbs))
	names := make(map[string]string, len(f.Bulbs))
	for _, sb := range f.Bulbs {
		if sb.DeviceID == "" {
			return nil, fmt.Errorf("registry %s contains a bulb with no device id", s.path)
		}
		if seen[sb.DeviceID] {
			return nil, fmt.Errorf("registry %s contains duplicate device id %q", s.path, sb.DeviceID)
		}
		if owner, dup := names[sb.Name]; dup {
			return nil, fmt.Errorf("registry %s reuses name %q for %s and %s", s.path, sb.Name, owner, sb.DeviceID)
		}
		seen[sb.DeviceID] = true
		names[sb.Name] = sb.DeviceID
		// Status is deliberately not persisted: nothing is assumed still online
		// across a restart. Everything loads disconnected and proves otherwise
		// by connecting.
		reg.bulbs[sb.DeviceID] = &bulb.Bulb{
			DeviceID:        sb.DeviceID,
			Name:            sb.Name,
			Status:          bulb.Disconnected,
			State:           sb.State,
			Capabilities:    sb.Capabilities,
			FirmwareVersion: sb.Firmware,
			FirstSeen:       sb.FirstSeen,
			LastSeen:        sb.LastSeen,
		}
	}
	return reg, nil
}

// schedule marks the registry dirty and arranges a debounced save.
func (s *Store) schedule() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = true
	if s.timer != nil {
		return
	}
	s.timer = time.AfterFunc(s.delay, func() {
		s.mu.Lock()
		s.timer = nil
		s.mu.Unlock()
		if err := s.Flush(); err != nil && s.OnError != nil {
			s.OnError(err)
		}
	})
}

// Flush writes the registry now, if anything changed since the last write.
func (s *Store) Flush() error {
	s.mu.Lock()
	reg, pending := s.reg, s.pending
	s.pending = false
	s.mu.Unlock()
	if reg == nil || !pending {
		return nil
	}
	return s.save(reg)
}

// save writes the registry atomically: a temporary file in the same directory,
// synced, then renamed over the target. A crash mid-write leaves the previous
// good file intact rather than a truncated one.
func (s *Store) save(reg *Registry) error {
	snapshot := reg.Snapshot()
	f := file{Version: FileVersion, Bulbs: make([]storedBulb, 0, len(snapshot))}
	for _, b := range snapshot {
		f.Bulbs = append(f.Bulbs, storedBulb{
			DeviceID:     b.DeviceID,
			Name:         b.Name,
			FirstSeen:    b.FirstSeen,
			LastSeen:     b.LastSeen,
			Capabilities: b.Capabilities,
			Firmware:     b.FirmwareVersion,
			State:        b.State,
		})
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding registry: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating registry directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".registry-*.json")
	if err != nil {
		return fmt.Errorf("creating temporary registry file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temporary registry file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temporary registry file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary registry file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replacing registry %s: %w", s.path, err)
	}
	return nil
}

// Close stops any pending debounce and writes one final time.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()
	return s.Flush()
}
