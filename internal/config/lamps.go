package config

import (
	"fmt"
	"strings"

	"github.com/mmichaelb/haigosmart/internal/control"
)

// ParseLamps reads the configured lamp set: comma-separated deviceID=name
// pairs, whitespace around either side ignored.
//
// Nothing is skipped. A malformed entry is reported by its position and its
// content and stops startup, because the alternative — dropping it — presents
// later as a room that stopped working, with a clean log and no explanation.
func ParseLamps(s string) ([]Lamp, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}

	entries := strings.Split(s, ",")
	lamps := make([]Lamp, 0, len(entries))
	byID := make(map[string]int, len(entries))
	byName := make(map[string]string, len(entries))

	for i, entry := range entries {
		pos := i + 1
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("HAIGOSMART_LAMPS entry %d is empty", pos)
		}
		id, name, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("HAIGOSMART_LAMPS entry %d %q is not deviceID=name", pos, entry)
		}
		id, name = strings.TrimSpace(id), strings.TrimSpace(name)
		if id == "" {
			return nil, fmt.Errorf("HAIGOSMART_LAMPS entry %d has an empty device id", pos)
		}
		if name == "" {
			return nil, fmt.Errorf("HAIGOSMART_LAMPS entry %d (%s) has an empty name", pos, id)
		}
		if first, dup := byID[id]; dup {
			return nil, fmt.Errorf("HAIGOSMART_LAMPS repeats device id %q at entries %d and %d", id, first, pos)
		}
		if owner, dup := byName[name]; dup {
			return nil, fmt.Errorf("HAIGOSMART_LAMPS reuses name %q for %s and %s", name, owner, id)
		}
		// A lamp named after a command could not be addressed from the terminal,
		// which would make the two modes disagree about what a name is for.
		if control.IsVerb(name) {
			return nil, fmt.Errorf("HAIGOSMART_LAMPS name %q is a terminal command; pick another so the name stays addressable", name)
		}

		byID[id] = pos
		byName[name] = id
		lamps = append(lamps, Lamp{DeviceID: id, Name: name})
	}
	return lamps, nil
}
