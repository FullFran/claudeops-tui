package usage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWithFileLockSerializesHolders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	var (
		mu        sync.Mutex
		inside    int
		maxInside int
		wg        sync.WaitGroup
	)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := withFileLock(path, func() error {
				mu.Lock()
				inside++
				if inside > maxInside {
					maxInside = inside
				}
				mu.Unlock()

				time.Sleep(2 * time.Millisecond)

				mu.Lock()
				inside--
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("withFileLock: %v", err)
			}
		}()
	}
	wg.Wait()

	if maxInside != 1 {
		t.Errorf("max concurrent holders = %d, want 1", maxInside)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Errorf("lock file not created: %v", err)
	}
}

func TestGetHoldsCredentialsLock(t *testing.T) {
	dir := t.TempDir()
	credsPath := writeCreds(t, dir, validCreds(time.Now().Add(time.Hour)))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"five_hour": map[string]any{"utilization": 1.0, "resets_at": "2026-04-08T18:59:59Z"},
		})
	}))
	defer srv.Close()

	acquired := make(chan struct{})
	release := make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- withFileLock(credsPath, func() error {
			close(acquired)
			<-release
			return nil
		})
	}()
	<-acquired

	c := New(credsPath)
	c.UsageURL = srv.URL
	done := make(chan error, 1)
	go func() {
		_, err := c.Get(context.Background())
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Get completed while the credentials lock was held (err=%v)", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Get after lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Get never completed after the lock was released")
	}
}

func TestWithFileLockPropagatesAndReleases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	wantErr := errors.New("boom")

	tests := []struct {
		name string
		fn   func() error
		want error
	}{
		{name: "propagates callback error", fn: func() error { return wantErr }, want: wantErr},
		{name: "lock is reusable after failure", fn: func() error { return nil }, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- withFileLock(path, tt.fn) }()
			select {
			case err := <-done:
				if !errors.Is(err, tt.want) {
					t.Errorf("err = %v, want %v", err, tt.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("withFileLock deadlocked; previous holder did not release")
			}
		})
	}
}
