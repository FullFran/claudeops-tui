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
	defer w.Close()

	if err := addDirsRecursively(w, c.root); err != nil {
		return err
	}

	// Throttle re-ingest of the same file.
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	dirty := map[string]bool{}

	flush := func() {
		if len(dirty) == 0 {
			return
		}
		offsets, _ := c.store.LoadOffsets()
		for p := range dirty {
			_ = c.ingestFile(ctx, p, offsets[p])
		}
		dirty = map[string]bool{}
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return ctx.Err()
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
				if filepath.Ext(ev.Name) == ".jsonl" {
					dirty[ev.Name] = true
				} else if isDir(ev.Name) {
					_ = w.Add(ev.Name)
				}
			}
		case <-w.Errors:
			// keep going
		case <-tick.C:
			flush()
		}
	}
}
