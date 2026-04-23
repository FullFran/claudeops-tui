package store

import (
	"context"
	"sync"
	"testing"
)

func TestConfigGetMissingKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	v, ok, err := s.ConfigGet(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("ConfigGet unexpected error: %v", err)
	}
	if ok {
		t.Errorf("ok should be false for missing key")
	}
	if v != "" {
		t.Errorf("value should be empty string for missing key, got %q", v)
	}
}

func TestConfigSetThenGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.ConfigSet(ctx, "mykey", "myval"); err != nil {
		t.Fatalf("ConfigSet: %v", err)
	}
	v, ok, err := s.ConfigGet(ctx, "mykey")
	if err != nil {
		t.Fatalf("ConfigGet: %v", err)
	}
	if !ok {
		t.Error("ok should be true for existing key")
	}
	if v != "myval" {
		t.Errorf("value: want %q got %q", "myval", v)
	}
}

func TestConfigSetUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tests := []struct {
		setValue string
		wantVal  string
	}{
		{"first", "first"},
		{"second", "second"},
		{"third", "third"},
	}

	for _, tc := range tests {
		t.Run("set_"+tc.setValue, func(t *testing.T) {
			if err := s.ConfigSet(ctx, "upsert_key", tc.setValue); err != nil {
				t.Fatalf("ConfigSet: %v", err)
			}
			v, ok, err := s.ConfigGet(ctx, "upsert_key")
			if err != nil {
				t.Fatalf("ConfigGet: %v", err)
			}
			if !ok {
				t.Error("ok should be true")
			}
			if v != tc.wantVal {
				t.Errorf("want %q got %q", tc.wantVal, v)
			}
		})
	}
}

func TestConfigGetEmptyKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Empty key — DB will not find anything, should return ("", false, nil)
	v, ok, err := s.ConfigGet(ctx, "")
	if err != nil {
		t.Fatalf("ConfigGet empty key: %v", err)
	}
	if ok {
		t.Error("ok should be false for empty key lookup with no data")
	}
	if v != "" {
		t.Errorf("value should be empty, got %q", v)
	}
}

func TestConfigConcurrentSetGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := "concurrent_key"
			val := "value"
			_ = s.ConfigSet(ctx, key, val)
			_, _, _ = s.ConfigGet(ctx, key)
			_ = s.ConfigSet(ctx, "key_unique_"+string(rune('A'+i)), "v")
		}()
	}
	wg.Wait()
}
