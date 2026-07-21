package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSuperviseWatchRecordsAWatcherThatDied(t *testing.T) {
	tests := []struct {
		name     string
		watchErr error
		cancel   bool
		wantNote bool
	}{
		{name: "watcher fails", watchErr: errors.New("inotify limit reached"), wantNote: true},
		{name: "watcher returns cleanly", wantNote: false},
		{name: "shutdown is not a failure", watchErr: context.Canceled, cancel: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			var h collectorHealth
			superviseWatch(ctx, "claude", func(context.Context) error { return tc.watchErr }, &h)

			notes := h.snapshot()
			if got := len(notes) == 1; got != tc.wantNote {
				t.Fatalf("recorded a note = %v, want %v (%v)", got, tc.wantNote, notes)
			}
			if tc.wantNote && !strings.Contains(notes[0], "inotify limit reached") {
				t.Errorf("note = %q, want the underlying error", notes[0])
			}
		})
	}
}

// fakeCounter reports a monotonically growing emit-error count, which is what a
// collector retrying the same unwritable line every 500ms looks like.
type fakeCounter struct{ n atomic.Int64 }

func (f *fakeCounter) EmitErrorCount() int64 { return f.n.Add(1) }

type flatCounter struct{}

func (flatCounter) EmitErrorCount() int64 { return 7 }

func TestSuperviseEmitErrorsReportsAPermanentStall(t *testing.T) {
	tests := []struct {
		name     string
		counter  emitCounter
		wantNote bool
	}{
		{name: "errors keep climbing", counter: &fakeCounter{}, wantNote: true},
		{name: "count holds steady", counter: flatCounter{}, wantNote: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			var h collectorHealth
			superviseEmitErrors(ctx, "codex", tc.counter, &h, time.Millisecond)

			notes := h.snapshot()
			if got := len(notes) > 0; got != tc.wantNote {
				t.Fatalf("recorded a note = %v, want %v (%v)", got, tc.wantNote, notes)
			}
			if tc.wantNote {
				if !strings.Contains(notes[0], "codex") || !strings.Contains(notes[0], "stall") {
					t.Errorf("note = %q, want it to name the source and the stall", notes[0])
				}
				if len(notes) != 1 {
					t.Errorf("a continuing stall must be reported once, got %d notes", len(notes))
				}
			}
		})
	}
}

func TestCollectorHealthWriteToStaysQuietWhenHealthy(t *testing.T) {
	var h collectorHealth
	var sb strings.Builder
	h.writeTo(&sb)
	if sb.String() != "" {
		t.Errorf("a healthy run must print nothing, got %q", sb.String())
	}
	h.add("claude", errors.New("boom"))
	h.writeTo(&sb)
	if !strings.Contains(sb.String(), "claude") || !strings.Contains(sb.String(), "boom") {
		t.Errorf("report = %q, want the source and the reason", sb.String())
	}
}
