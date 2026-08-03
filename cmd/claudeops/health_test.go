package main

import (
	"context"
	"errors"
	"strings"
	"sync"
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

// stickyPoller always reports the same failure, the way a schema change or a
// revoked permission would. failures is how many polls have failed in a row.
type stickyPoller struct {
	err      error
	failures int
}

func (p stickyPoller) LastErr() error { return p.err }
func (p stickyPoller) ConsecutiveFailures() int {
	if p.err == nil {
		return 0
	}
	return p.failures
}

// slowPollPoller is the case the watchdog exists to get right: ONE poll has
// failed and it is taking a long time, so the watchdog samples that same stale
// error over and over. Counting samples would call this a permanent outage.
// Counting polls does not.
type slowPollPoller struct{}

func (slowPollPoller) LastErr() error           { return errors.New("database is locked") }
func (slowPollPoller) ConsecutiveFailures() int { return 1 }

// healingPoller fails twice and then recovers, the way a locked database does.
// Its failure count is driven by polls, not by how often it is asked — in
// production those are two independent clocks.
type healingPoller struct {
	mu       sync.Mutex
	polls    int
	failures int
	err      error
}

// poll advances the poller's own clock, independently of the watchdog's.
func (p *healingPoller) poll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.polls++
	if p.polls <= stallSamples-1 {
		p.failures++
		p.err = errors.New("database is locked")
		return
	}
	p.failures, p.err = 0, nil
}

func (p *healingPoller) LastErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *healingPoller) ConsecutiveFailures() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failures
}

func TestSupervisePollErrorsReportsOnlyPermanentFailures(t *testing.T) {
	healing := &healingPoller{}
	// Drive the poller through its transient outage before the watchdog ever
	// looks, so the two clocks stay genuinely independent.
	for i := 0; i < stallSamples+2; i++ {
		healing.poll()
	}

	tests := []struct {
		name     string
		poller   pollReporter
		wantNote bool
	}{
		{
			name:     "failure never clears",
			poller:   stickyPoller{err: errors.New("no such table: message"), failures: stallSamples},
			wantNote: true,
		},
		{name: "failure clears on its own", poller: healing, wantNote: false},
		{name: "one slow poll is not an outage", poller: slowPollPoller{}, wantNote: false},
		{name: "polling is healthy", poller: stickyPoller{}, wantNote: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			var h collectorHealth
			supervisePollErrors(ctx, "opencode", tc.poller, &h, time.Millisecond)

			notes := h.snapshot()
			if got := len(notes) > 0; got != tc.wantNote {
				t.Fatalf("recorded a note = %v, want %v (%v)", got, tc.wantNote, notes)
			}
			if !tc.wantNote {
				return
			}
			if !strings.Contains(notes[0], "opencode") {
				t.Errorf("note = %q, want it to name the source", notes[0])
			}
			if !strings.Contains(notes[0], "no such table: message") {
				t.Errorf("note = %q, want it to carry the underlying failure", notes[0])
			}
			if len(notes) != 1 {
				t.Errorf("a continuing failure must be reported once, got %d notes", len(notes))
			}
		})
	}
}
