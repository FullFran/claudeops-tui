package collector

import (
	"context"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch ingests existing data, then watches `root` recursively for new files
// and appended bytes. Returns when ctx is done.
func (c *Collector) Watch(ctx context.Context) error {
	if err := c.IngestExisting(ctx); err != nil {
		return err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()

	if err := addDirsRecursively(w, c.root); err != nil {
		return err
	}
	if c.watchReady != nil {
		c.watchReady()
	}

	// Throttle re-ingest of the same file.
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	dirty := map[string]bool{}

	for {
		select {
		case <-ctx.Done():
			c.drainOnShutdown(ctx, dirty)
			return ctx.Err()
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
				if filepath.Ext(ev.Name) == ".jsonl" {
					dirty[ev.Name] = true
				} else if isDir(ev.Name) {
					// Watch the new tree and pick up files written into it
					// before the watch was in place.
					_ = addDirsRecursively(w, ev.Name)
					for _, p := range jsonlFilesUnder(ev.Name) {
						dirty[p] = true
					}
				}
			}
		case <-w.Errors:
			// keep going
		case <-tick.C:
			c.flushDirty(ctx, dirty)
			dirty = map[string]bool{}
		}
	}
}

// flushDirty ingests every file marked dirty since the last flush.
func (c *Collector) flushDirty(ctx context.Context, dirty map[string]bool) {
	if len(dirty) == 0 {
		return
	}
	offsets, _ := c.loadOffsets()
	for p := range dirty {
		if err := c.ingestFile(ctx, p, offsets[p]); err != nil && ctx.Err() == nil {
			c.fileErrors.Add(1)
		}
	}
}

// drainOnShutdown runs the last flush on a fresh bounded context: the caller's
// ctx is already cancelled, so reusing it would ingest nothing.
func (c *Collector) drainOnShutdown(ctx context.Context, dirty map[string]bool) {
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	c.flushDirty(dctx, dirty)
}
