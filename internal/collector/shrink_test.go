package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestShrunkFileResetsOffset covers #37: truncation, rotation or an editor
// atomic save leaves the stored offset past EOF; the file must be re-read from
// the beginning instead of being ignored forever.
func TestShrunkFileResetsOffset(t *testing.T) {
	ctx := context.Background()
	const replacement = `{"type":"user","uuid":"u-new","sessionId":"s2","cwd":"/p","timestamp":"2026-04-08T14:00:00Z"}` + "\n"

	tests := []struct {
		name    string
		rewrite func(t *testing.T, path string)
	}{
		{
			name: "truncated in place",
			rewrite: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(replacement), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "replaced by an atomic save",
			rewrite: func(t *testing.T, path string) {
				t.Helper()
				tmp := filepath.Join(filepath.Dir(path), "tmp-atomic")
				if err := os.WriteFile(tmp, []byte(replacement), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(tmp, path); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, s, root := newTestCollector(t)
			p := writeJSONL(t, root, "-tmp-shrink", "a.jsonl", sampleAssistant+sampleUser)
			if err := c.IngestExisting(ctx); err != nil {
				t.Fatal(err)
			}
			if c.IngestedCount() != 2 {
				t.Fatalf("first pass: want 2 got %d", c.IngestedCount())
			}

			tc.rewrite(t, p)

			if err := c.IngestExisting(ctx); err != nil {
				t.Fatal(err)
			}
			var n int
			if err := s.DB().QueryRow(`SELECT COUNT(*) FROM events WHERE uuid='u-new'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Errorf("post-shrink content never ingested: want 1 row got %d", n)
			}
			offsets, _ := s.LoadOffsets()
			if got, want := offsets[p], int64(len(replacement)); got != want {
				t.Errorf("offset: want %d got %d", want, got)
			}
		})
	}
}
