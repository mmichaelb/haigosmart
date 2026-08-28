package logging

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

var start = time.Date(2026, 8, 28, 14, 2, 0, 0, time.Local)

// decode reads the one record in buf.
func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("record is not JSON: %v\n%s", err, buf.String())
	}
	return rec
}

// TestCompactTime pins the timestamp layout in contracts/log-records.md. The
// assertion is on the emitted JSON, not on a formatting helper: the helper being
// right is not the claim, the record being right is.
func TestCompactTime(t *testing.T) {
	var buf bytes.Buffer
	at := time.Date(2026, 8, 28, 14, 3, 12, 123_000_000, time.Local)
	log := newAt(&buf, slog.LevelInfo, start, func() time.Time { return at })
	log.Info("bulb connected")

	rec := decode(t, &buf)
	if got := rec["time"]; got != "2026-08-28 14:03:12.123" {
		t.Errorf("time = %v, want %q", got, "2026-08-28 14:03:12.123")
	}
}

// TestSince covers the second half of the format the spec asked for: the
// difference, measured from process start.
func TestSince(t *testing.T) {
	for _, tc := range []struct {
		elapsed time.Duration
		want    string
	}{
		{0, "0s"},
		{72_345 * time.Millisecond, "1m12.345s"},
		{1500 * time.Microsecond, "1ms"}, // truncated to milliseconds
		{90 * time.Minute, "1h30m0s"},
	} {
		var buf bytes.Buffer
		at := start.Add(tc.elapsed)
		log := newAt(&buf, slog.LevelInfo, start, func() time.Time { return at })
		log.Info("bulb connected")

		if got := decode(t, &buf)["since"]; got != tc.want {
			t.Errorf("after %s: since = %v, want %q", tc.elapsed, got, tc.want)
		}
	}
}

// TestSinceOnEveryRecordFromEveryGoroutine is why `since` is measured from the
// process start rather than from the previous record: it has to be well defined
// when many goroutines log at once. Run under -race.
//
// Note what is *not* asserted: that `since` rises down the file. It cannot. A
// record is stamped when the log call happens and written when its goroutine
// wins the writer's lock, so a record with an earlier timestamp can land after
// one with a later timestamp. That is exactly the reordering that makes a
// difference-from-the-previous-record field meaningless, and the reason this
// field is anchored to the process start instead.
func TestSinceOnEveryRecordFromEveryGoroutine(t *testing.T) {
	var mu sync.Mutex
	buf := &bytes.Buffer{}
	begin := time.Now()
	log := New(&lockedWriter{mu: &mu, w: buf}, slog.LevelInfo, begin)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				log.Info("bulb reported state", "worker", i)
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(begin)

	mu.Lock()
	defer mu.Unlock()
	lines := 0
	sc := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	for sc.Scan() {
		lines++
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("line %d is not JSON: %v", lines, err)
		}
		raw, ok := rec["since"].(string)
		if !ok {
			t.Fatalf("line %d has no since field: %s", lines, sc.Text())
		}
		d, err := time.ParseDuration(raw)
		if err != nil {
			t.Fatalf("line %d: since %q does not parse: %v", lines, raw, err)
		}
		if d < 0 || d > elapsed {
			t.Errorf("line %d: since = %s, outside the run's own [0, %s]", lines, d, elapsed)
		}
	}
	if lines != 200 {
		t.Errorf("got %d records, want 200", lines)
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

// TestFatalWriterExits covers the spec's edge case: an unattended server whose
// records go nowhere is not running, so it must stop rather than continue deaf.
func TestFatalWriterExits(t *testing.T) {
	var stderr bytes.Buffer
	var codes []int

	w := &FatalWriter{
		W:      failingWriter{err: errors.New("broken pipe")},
		Stderr: &stderr,
		Exit:   func(code int) { codes = append(codes, code) },
	}
	log := New(w, slog.LevelInfo, time.Now())
	log.Info("bulb connected")

	if len(codes) != 1 {
		t.Fatalf("exit called %d times, want 1", len(codes))
	}
	if codes[0] == 0 {
		t.Errorf("exit status %d, want non-zero", codes[0])
	}
	if !strings.Contains(stderr.String(), "broken pipe") {
		t.Errorf("stderr = %q, want the underlying error", stderr.String())
	}
}

// TestFatalWriterReportsOnce keeps a failing stream from turning into a flood on
// the one channel still working.
func TestFatalWriterReportsOnce(t *testing.T) {
	var stderr bytes.Buffer
	var calls int

	w := &FatalWriter{
		W:      failingWriter{err: errors.New("broken pipe")},
		Stderr: &stderr,
		Exit:   func(int) { calls++ },
	}
	log := New(w, slog.LevelInfo, time.Now())
	for range 5 {
		log.Info("bulb connected")
	}

	if calls != 1 {
		t.Errorf("exit called %d times, want 1", calls)
	}
	if n := strings.Count(stderr.String(), "broken pipe"); n != 1 {
		t.Errorf("reported %d times on stderr, want 1", n)
	}
}
