package lights_test

import (
	"os/exec"
	"strings"
	"testing"
)

// internal/lights must not import a front-end. A headless server exists
// precisely so it can run without a terminal UI, and an import here would drag
// Bubble Tea into it — and would also make the Home Assistant bridge depend on
// the terminal it is meant to be a sibling of.
func TestLightsImportsNoFrontEnd(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "haigosmart/internal/lights").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	forbidden := []string{
		"haigosmart/internal/control",
		"haigosmart/internal/tui",
		"haigosmart/internal/hass",
		"github.com/charmbracelet",
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, bad := range forbidden {
			if strings.HasPrefix(dep, bad) {
				t.Errorf("internal/lights depends on %s; the core must not import a front-end", dep)
			}
		}
	}
}
