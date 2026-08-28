package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// build compiles the command with the given ldflags and returns the path. The
// tests below go through a real binary rather than calling a function, because
// what is being tested is partly *ordering* — that the version answer comes out
// before configuration is loaded — and a function call cannot show that.
func build(t *testing.T, ldflags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "haigosmartd")
	args := []string{"build", "-o", bin}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, ".")
	cmd := exec.Command("go", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the command: %v\n%s", err, out)
	}
	return bin
}

func TestVersionFlagReportsDevWhenUnstamped(t *testing.T) {
	out, err := exec.Command(build(t, ""), "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("-version exited non-zero: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "haigosmartd dev" {
		t.Errorf("-version printed %q, want %q", got, "haigosmartd dev")
	}
}

func TestVersionFlagReportsTheStampedVersion(t *testing.T) {
	out, err := exec.Command(build(t, "-X main.version=1.2.3"), "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("-version exited non-zero: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "haigosmartd 1.2.3" {
		t.Errorf("-version printed %q, want the stamped %q", got, "haigosmartd 1.2.3")
	}
}

// TestVersionFlagWorksWithAnUnusableConfiguration is the ordering test. "Which
// build is this?" is the first question asked about a deployment that will not
// start, so the answer must not depend on the configuration being valid.
func TestVersionFlagWorksWithAnUnusableConfiguration(t *testing.T) {
	cmd := exec.Command(build(t, ""), "-version")
	cmd.Env = append(os.Environ(),
		"HAIGOSMART_CT_MIN_KELVIN=7000", // above the maximum: Validate rejects this
		"HAIGOSMART_HEADLESS=true",      // headless with no lamps: also rejected
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("-version exited non-zero with a bad configuration: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "haigosmartd dev" {
		t.Errorf("-version printed %q, want %q", got, "haigosmartd dev")
	}
}
