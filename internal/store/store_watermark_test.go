package store

import (
	"path/filepath"
	"testing"
)

// TestSourceWatermark verifies that source_watermarks table is created on Open(),
// that SaveSourceWatermark / LoadSourceWatermark round-trip correctly,
// and that the table is idempotent (safe to migrate twice).
func TestSourceWatermark(t *testing.T) {
	t.Run("table created on open", func(t *testing.T) {
		dir := t.TempDir()
		s, err := Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = s.Close() }()

		rows, err := s.db.Query("PRAGMA table_info(source_watermarks)")
		if err != nil {
			t.Fatalf("PRAGMA: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var cols []string
		for rows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt interface{}
			if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				t.Fatalf("scan: %v", err)
			}
			cols = append(cols, name)
		}
		if len(cols) == 0 {
			t.Fatal("source_watermarks table has no columns (table not created)")
		}
		wantCols := map[string]bool{"source": true, "position": true}
		for _, c := range cols {
			delete(wantCols, c)
		}
		if len(wantCols) > 0 {
			t.Errorf("missing columns: %v", wantCols)
		}
	})

	t.Run("save and load round-trip", func(t *testing.T) {
		dir := t.TempDir()
		s, err := Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = s.Close() }()

		// Initially empty — LoadSourceWatermark returns "".
		pos, err := s.LoadSourceWatermark("opencode")
		if err != nil {
			t.Fatalf("LoadSourceWatermark initial: %v", err)
		}
		if pos != "" {
			t.Errorf("expected empty watermark, got %q", pos)
		}

		// Save a watermark.
		if err := s.SaveSourceWatermark("opencode", "1779000000000"); err != nil {
			t.Fatalf("SaveSourceWatermark: %v", err)
		}

		pos, err = s.LoadSourceWatermark("opencode")
		if err != nil {
			t.Fatalf("LoadSourceWatermark after save: %v", err)
		}
		if pos != "1779000000000" {
			t.Errorf("expected %q, got %q", "1779000000000", pos)
		}
	})

	t.Run("overwrite updates the value", func(t *testing.T) {
		dir := t.TempDir()
		s, err := Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = s.Close() }()

		_ = s.SaveSourceWatermark("opencode", "100")
		_ = s.SaveSourceWatermark("opencode", "200")

		pos, err := s.LoadSourceWatermark("opencode")
		if err != nil {
			t.Fatalf("LoadSourceWatermark: %v", err)
		}
		if pos != "200" {
			t.Errorf("expected %q, got %q", "200", pos)
		}
	})

	t.Run("different sources are independent", func(t *testing.T) {
		dir := t.TempDir()
		s, err := Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = s.Close() }()

		_ = s.SaveSourceWatermark("opencode", "aaa")
		_ = s.SaveSourceWatermark("codex", "bbb")

		posOC, _ := s.LoadSourceWatermark("opencode")
		posCX, _ := s.LoadSourceWatermark("codex")
		if posOC != "aaa" {
			t.Errorf("opencode: expected %q got %q", "aaa", posOC)
		}
		if posCX != "bbb" {
			t.Errorf("codex: expected %q got %q", "bbb", posCX)
		}
	})

	t.Run("idempotent migrate — open twice", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "test.db")

		s1, err := Open(dbPath)
		if err != nil {
			t.Fatalf("first Open: %v", err)
		}
		_ = s1.SaveSourceWatermark("opencode", "xyz")
		s1.Close()

		s2, err := Open(dbPath)
		if err != nil {
			t.Fatalf("second Open: %v", err)
		}
		defer func() { _ = s2.Close() }()

		pos, err := s2.LoadSourceWatermark("opencode")
		if err != nil {
			t.Fatalf("LoadSourceWatermark: %v", err)
		}
		if pos != "xyz" {
			t.Errorf("expected %q, got %q", "xyz", pos)
		}
	})
}
