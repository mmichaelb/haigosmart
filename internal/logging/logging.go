// Package logging builds the process's structured logger.
//
// Every record is one JSON object on one line, carrying a compact timestamp and
// the time elapsed since the process started. The format is fixed by
// specs/003-headless-deployment/contracts/log-records.md; in headless mode this
// is the entire content of standard output, so a collector can read it without
// parsing prose and without a parser of ours.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// TimeLayout is the record timestamp: the date and the time, without RFC3339's
// `T` and zone offset. Twenty-three characters instead of thirty, still
// sortable, still unambiguous, and readable without mental arithmetic. The year
// stays because records outlive the session that produced them.
const TimeLayout = "2006-01-02 15:04:05.000"

// New returns a logger writing JSON lines to w at the given level. Records carry
// `since`, the time elapsed from start, so "how far into this run did that
// happen?" is answerable from one record rather than from two.
func New(w io.Writer, level slog.Level, start time.Time) *slog.Logger {
	return newAt(w, level, start, nil)
}

// newAt is New with a clock, so tests can pin the timestamp.
func newAt(w io.Writer, level slog.Level, start time.Time, now func() time.Time) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: compactTime,
	})
	return slog.New(&elapsedHandler{Handler: base, start: start, now: now})
}

// compactTime rewrites the record timestamp to TimeLayout. Local time is
// deliberate: this runs in one household, where the operator compares a record
// against the moment a lamp visibly changed. A fleet would want UTC.
func compactTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
		return slog.String(slog.TimeKey, a.Value.Time().Format(TimeLayout))
	}
	return a
}

// elapsedHandler adds `since` to every record.
//
// It cannot be a logger-level attribute, because its value differs per record;
// and it is measured from the process start rather than from the previous
// record because records are produced from many goroutines at once. A
// difference-from-previous would depend on which goroutine won a race, so the
// same run would print different numbers.
type elapsedHandler struct {
	slog.Handler
	start time.Time
	now   func() time.Time // nil means use the record's own timestamp
}

func (h *elapsedHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.now != nil {
		r.Time = h.now()
	}
	r.AddAttrs(slog.String("since", r.Time.Sub(h.start).Truncate(time.Millisecond).String()))
	return h.Handler.Handle(ctx, r)
}

func (h *elapsedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &elapsedHandler{Handler: h.Handler.WithAttrs(attrs), start: h.start, now: h.now}
}

func (h *elapsedHandler) WithGroup(name string) slog.Handler {
	return &elapsedHandler{Handler: h.Handler.WithGroup(name), start: h.start, now: h.now}
}

// FatalWriter stops the process when the record stream cannot be written.
//
// slog discards handler write errors by design, which is right for a logger and
// wrong for an unattended server: an instance whose output goes nowhere is not
// running, it is only consuming electricity. One line on standard error, one
// non-zero exit, and the restart decision belongs to whatever supervises it.
type FatalWriter struct {
	W      io.Writer
	Stderr io.Writer
	Exit   func(int)

	once sync.Once
}

func (f *FatalWriter) Write(p []byte) (int, error) {
	n, err := f.W.Write(p)
	if err != nil {
		f.once.Do(func() {
			fmt.Fprintf(f.Stderr, "haigosmart: cannot write the record stream: %v\n", err)
			f.Exit(1)
		})
	}
	return n, err
}
