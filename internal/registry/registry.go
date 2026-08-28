// Package registry holds the authoritative set of known bulbs and persists it
// across restarts.
package registry

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
)

// Registry is the in-memory set of bulbs, safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	bulbs map[string]*bulb.Bulb
	dirty func() // called after any mutation, to schedule a save
}

// New returns an empty registry. onChange is invoked after every mutation so
// the caller can schedule persistence; it may be nil.
func New(onChange func()) *Registry {
	if onChange == nil {
		onChange = func() {}
	}
	return &Registry{bulbs: make(map[string]*bulb.Bulb), dirty: onChange}
}

// Errors returned by target resolution. They carry the guidance the operator
// needs, per contracts/tui-commands.md.
type (
	// UnknownTargetError means nothing matched.
	UnknownTargetError struct{ Target string }
	// AmbiguousTargetError means a prefix matched more than one bulb.
	AmbiguousTargetError struct {
		Target     string
		Candidates []string
	}
	// NameInUseError means a name is already taken by another bulb.
	NameInUseError struct {
		Name  string
		Owner string
	}
)

func (e UnknownTargetError) Error() string {
	return fmt.Sprintf("unknown bulb %q: no bulb by that name or id. run `list` to see registered bulbs", e.Target)
}

func (e AmbiguousTargetError) Error() string {
	return fmt.Sprintf("ambiguous target %q: matches %s. use a longer prefix", e.Target, strings.Join(e.Candidates, ", "))
}

func (e NameInUseError) Error() string {
	return fmt.Sprintf("name %q already used by %s", e.Name, e.Owner)
}

// Upsert registers a connecting bulb, or updates the entry it already has.
// A returning bulb always rejoins its existing entry, so power cycles never
// create duplicates. The bool reports whether this bulb is newly discovered.
func (r *Registry) Upsert(deviceID, remoteAddr string, caps bulb.Capabilities, now time.Time) (bulb.Bulb, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.dirty()

	if existing, ok := r.bulbs[deviceID]; ok {
		existing.LastSeen = now
		existing.RemoteAddr = remoteAddr
		if caps.Known || !existing.Capabilities.Known {
			existing.Capabilities = caps
		}
		if existing.Status != bulb.Discovered {
			existing.Status = bulb.Connected
		}
		return *existing, false
	}
	b := &bulb.Bulb{
		DeviceID:     deviceID,
		Name:         deviceID,
		Status:       bulb.Discovered,
		Capabilities: caps,
		FirstSeen:    now,
		LastSeen:     now,
		RemoteAddr:   remoteAddr,
	}
	r.bulbs[deviceID] = b
	return *b, true
}

// Declare records a lamp the configuration says this instance is responsible
// for, whether or not it has ever connected.
//
// This is what makes the configured set authoritative rather than advisory: a
// declared lamp exists from startup — named, adopted, and disconnected until it
// proves otherwise — so Home Assistant shows it immediately instead of waiting
// for it to appear, and a lost registry file costs nothing but the last known
// state, which the lamp reports again on connecting.
//
// Declaring is not a reset. Everything already learned from the lamp — its
// state, capabilities, firmware, and first-seen time — survives, because the
// configuration says which lamps to serve, not what they are.
func (r *Registry) Declare(deviceID, name string) (created, renamed bool, err error) {
	if strings.TrimSpace(deviceID) == "" {
		return false, false, errors.New("device id must not be empty")
	}
	if strings.TrimSpace(name) == "" {
		return false, false, errors.New("name must not be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing := r.bulbs[deviceID]
	for _, other := range r.bulbs {
		if other != existing && other.Name == name {
			return false, false, NameInUseError{Name: name, Owner: other.DeviceID}
		}
	}

	if existing == nil {
		r.bulbs[deviceID] = &bulb.Bulb{
			DeviceID: deviceID,
			Name:     name,
			Status:   bulb.Disconnected,
		}
		r.dirty()
		return true, false, nil
	}

	if existing.Name != name {
		existing.Name = name
		renamed = true
	}
	// A lamp loaded from the registry file but never adopted is adopted now:
	// naming it in the configuration is the same deliberate act as naming it in
	// the terminal.
	if existing.Status == bulb.Discovered {
		existing.Status = bulb.Connected
	}
	if renamed {
		r.dirty()
	}
	return false, renamed, nil
}

// View returns a snapshot of one bulb. Callers get a copy rather than a pointer
// into the registry: shared mutable state read without the lock is a data race,
// and handing out pointers makes that race the default rather than the mistake.
func (r *Registry) View(deviceID string) (bulb.Bulb, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.bulbs[deviceID]
	if !ok {
		return bulb.Bulb{}, false
	}
	return *b, true
}

// List returns a snapshot of every bulb, ordered by name for stable display.
func (r *Registry) List() []bulb.Bulb {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]bulb.Bulb, 0, len(r.bulbs))
	for _, b := range r.bulbs {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Resolve finds the single bulb a target string refers to. Resolution order is
// exact name, exact device id, then unique case-insensitive prefix of either.
// An ambiguous prefix is an error listing the candidates; it is never a guess.
func (r *Registry) Resolve(target string) (bulb.Bulb, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if target == "" {
		return bulb.Bulb{}, UnknownTargetError{Target: target}
	}
	for _, b := range r.bulbs {
		if b.Name == target {
			return *b, nil
		}
	}
	if b, ok := r.bulbs[target]; ok {
		return *b, nil
	}
	lower := strings.ToLower(target)
	var matches []*bulb.Bulb
	for _, b := range r.bulbs {
		if strings.HasPrefix(strings.ToLower(b.Name), lower) || strings.HasPrefix(strings.ToLower(b.DeviceID), lower) {
			matches = append(matches, b)
		}
	}
	switch len(matches) {
	case 0:
		return bulb.Bulb{}, UnknownTargetError{Target: target}
	case 1:
		return *matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, b := range matches {
			names = append(names, b.Name)
		}
		sort.Strings(names)
		return bulb.Bulb{}, AmbiguousTargetError{Target: target, Candidates: names}
	}
}

// Rename assigns a new display name. Naming a discovered bulb adopts it: the
// bulb becomes controllable and is persisted. There is no separate adopt verb.
// The bool reports whether this rename was an adoption.
func (r *Registry) Rename(deviceID, name string) (adopted bool, err error) {
	if strings.TrimSpace(name) == "" {
		return false, errors.New("name must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bulbs[deviceID]
	if !ok {
		return false, UnknownTargetError{Target: deviceID}
	}
	for _, other := range r.bulbs {
		if other != b && other.Name == name {
			return false, NameInUseError{Name: name, Owner: other.DeviceID}
		}
	}
	adopted = b.Status == bulb.Discovered
	b.Name = name
	if adopted {
		b.Status = bulb.Connected
	}
	r.dirty()
	return adopted, nil
}

// SetState records a state the bulb reported and returns what actually changed.
// Reported state always wins over any commanded value, so a wall-switch change
// is never overwritten by what we last asked for.
func (r *Registry) SetState(deviceID string, state bulb.LightState, now time.Time) []bulb.FieldChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bulbs[deviceID]
	if !ok {
		return nil
	}
	changes := b.State.Diff(state)
	b.State = state
	b.LastSeen = now
	b.Desired = nil
	if len(changes) > 0 {
		r.dirty()
	}
	return changes
}

// SetDesired records what was last commanded, for divergence display only.
func (r *Registry) SetDesired(deviceID string, want bulb.LightState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.bulbs[deviceID]; ok {
		b.Desired = &want
	}
}

// Touch records that we heard from a bulb, without any state change.
func (r *Registry) Touch(deviceID string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.bulbs[deviceID]; ok {
		b.LastSeen = now
	}
}

// SetFirmware records the version string a bulb reported. It is displayed on
// the Home Assistant device card and is where the model name comes from.
func (r *Registry) SetFirmware(deviceID, version string) {
	if version == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.bulbs[deviceID]; ok && b.FirmwareVersion != version {
		b.FirmwareVersion = version
		r.dirty()
	}
}

// SetCapabilities records what a bulb can do. It never downgrades a known
// answer to an unknown one: a later connection that could not classify the bulb
// must not erase what an earlier one established. It reports whether anything
// changed.
func (r *Registry) SetCapabilities(deviceID string, caps bulb.Capabilities) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bulbs[deviceID]
	if !ok || (!caps.Known && b.Capabilities.Known) || b.Capabilities == caps {
		return false
	}
	b.Capabilities = caps
	r.dirty()
	return true
}

// SetDriver attaches a live connection to a bulb and reports the driver it
// replaced, if any. A non-nil previous driver means two connections claim the
// same device id.
func (r *Registry) SetDriver(deviceID string, d bulb.Driver) (previous bulb.Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bulbs[deviceID]
	if !ok {
		return nil
	}
	previous = b.Driver
	b.Driver = d
	if b.Status == bulb.Disconnected {
		b.Status = bulb.Connected
	}
	return previous
}

// Driver returns the live connection for a bulb, or nil when it is offline.
func (r *Registry) Driver(deviceID string) bulb.Driver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b, ok := r.bulbs[deviceID]; ok {
		return b.Driver
	}
	return nil
}

// Disconnect marks a bulb offline and drops its driver. driver identifies the
// connection tearing down: a late teardown from a replaced connection is
// ignored so it cannot knock out the live one.
func (r *Registry) Disconnect(deviceID string, driver bulb.Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bulbs[deviceID]
	if !ok || (driver != nil && b.Driver != driver) {
		return
	}
	b.Driver = nil
	if b.Status == bulb.Connected {
		b.Status = bulb.Disconnected
	}
	r.dirty()
}

// Snapshot is an alias for List, kept for the persistence layer's clarity.
func (r *Registry) Snapshot() []bulb.Bulb { return r.List() }
