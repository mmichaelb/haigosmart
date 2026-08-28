package config

import (
	"strings"
	"testing"
)

// TestParseLamps is a table over every row of the lamp-set table in
// contracts/configuration.md. The strings are the contract; this is where it is
// enforced.
func TestParseLamps(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		want    []Lamp
		wantAll []string // substrings the error must contain
	}{
		{
			name: "two lamps",
			in:   "a1=lamp,b2=desk",
			want: []Lamp{{"a1", "lamp"}, {"b2", "desk"}},
		},
		{
			name: "whitespace is trimmed",
			in:   " a1 = lamp , b2 = desk ",
			want: []Lamp{{"a1", "lamp"}, {"b2", "desk"}},
		},
		{
			name: "empty is no lamps, not an error",
			in:   "",
			want: nil,
		},
		{
			name:    "trailing comma",
			in:      "a1=lamp,",
			wantAll: []string{"HAIGOSMART_LAMPS", "entry 2", "empty"},
		},
		{
			name:    "no equals sign",
			in:      "a1",
			wantAll: []string{"HAIGOSMART_LAMPS", "entry 1", `"a1"`, "deviceID=name"},
		},
		{
			name:    "empty device id",
			in:      "=lamp",
			wantAll: []string{"HAIGOSMART_LAMPS", "entry 1", "empty device id"},
		},
		{
			name:    "empty name",
			in:      "a1=",
			wantAll: []string{"HAIGOSMART_LAMPS", "entry 1", "a1", "empty name"},
		},
		{
			name:    "duplicate device id",
			in:      "a1=lamp,a1=desk",
			wantAll: []string{"HAIGOSMART_LAMPS", `"a1"`, "entries 1 and 2"},
		},
		{
			name:    "duplicate name",
			in:      "a1=lamp,b2=lamp",
			wantAll: []string{"HAIGOSMART_LAMPS", `"lamp"`, "a1", "b2"},
		},
		{
			name:    "name is a terminal command",
			in:      "a1=off",
			wantAll: []string{"HAIGOSMART_LAMPS", `"off"`, "terminal command"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseLamps(tc.in)
			if len(tc.wantAll) > 0 {
				if err == nil {
					t.Fatalf("ParseLamps(%q) was accepted, giving %v", tc.in, got)
				}
				for _, want := range tc.wantAll {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not mention %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLamps(%q): %v", tc.in, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("lamp %d = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestParseLampsNeverSkips: every malformed entry is reported. A silently
// dropped lamp presents as a room that stopped working, with a clean log.
func TestParseLampsNeverSkips(t *testing.T) {
	got, err := ParseLamps("a1=good,,b2=alsogood")
	if err == nil {
		t.Fatalf("the empty middle entry was skipped, giving %v", got)
	}
}
