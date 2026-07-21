package main

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// collectorHealth records why ingestion stopped or stalled. Without it a dead
// watcher or a permanently failing write looks exactly like an idle machine:
// the dashboard simply stops moving and the user is never told why.
//
// The notes are written out once the TUI has released the alternate screen —
// printing to stderr while it is up would scribble over the display.
type collectorHealth struct {
	mu    sync.Mutex
	notes []string
}

// add records one failure, tagged with the source that produced it.
func (h *collectorHealth) add(source string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notes = append(h.notes, fmt.Sprintf("source %s: %v", source, err))
}

// snapshot returns a copy of the notes recorded so far.
func (h *collectorHealth) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.notes...)
}

// writeTo prints the recorded failures, or nothing at all on a healthy run.
func (h *collectorHealth) writeTo(w io.Writer) {
	notes := h.snapshot()
	if len(notes) == 0 {
		return
	}
	fmt.Fprintf(w, "claudeops: ingestion reported %d problem(s) during this session:\n", len(notes))
	for _, n := range notes {
		fmt.Fprintf(w, "  %s\n", n)
	}
	fmt.Fprintln(w, "  data may be incomplete — run `claudeops ingest` to catch up")
}

// superviseWatch runs a watch loop and records the error it died with. A
// cancelled context is an ordinary shutdown, not a failure.
func superviseWatch(ctx context.Context, source string, watch func(context.Context) error, h *collectorHealth) {
	err := watch(ctx)
	if err == nil || ctx.Err() != nil {
		return
	}
	h.add(source, fmt.Errorf("watcher stopped, ingestion is no longer live: %w", err))
}

// emitCounter is the slice of *collector.Collector the stall watchdog reads.
type emitCounter interface {
	EmitErrorCount() int64
}

const (
	// stallInterval is how often the emit-error counter is sampled.
	stallInterval = 5 * time.Second
	// stallSamples is how many consecutive growing samples mark a stall. #35
	// holds a file's offset at an event whose write failed, so nothing is lost
	// — but a failure that never clears retries the same line every 500ms
	// forever, and the counter is the only symptom. Requiring a streak keeps a
	// burst of rejected lines during a cold ingest from raising a false alarm.
	stallSamples = 3
)

// superviseEmitErrors watches a collector's emit-error counter and records a
// stall when it grows for stallSamples consecutive intervals. It reports once
// per stall, not once per sample, so a long outage produces one note.
func superviseEmitErrors(ctx context.Context, source string, c emitCounter, h *collectorHealth, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	prev := c.EmitErrorCount()
	streak := 0
	reported := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		cur := c.EmitErrorCount()
		if cur > prev {
			streak++
		} else {
			streak, reported = 0, false
		}
		prev = cur
		if streak >= stallSamples && !reported {
			h.add(source, fmt.Errorf(
				"ingestion looks stalled: %d failed writes and still retrying the same event", cur))
			reported = true
		}
	}
}
