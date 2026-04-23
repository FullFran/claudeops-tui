package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPost(t *testing.T) {
	// Replace sleep to avoid real delays in tests.
	origSleep := sleepFn
	t.Cleanup(func() { sleepFn = origSleep })
	sleepFn = func(time.Duration) {}

	tests := []struct {
		name         string
		statusCodes  []int // server responds with these codes in sequence
		wantErr      bool
		wantErrMsg   string
		wantAttempts int
	}{
		{
			name:         "200 success",
			statusCodes:  []int{200},
			wantErr:      false,
			wantAttempts: 1,
		},
		{
			name:         "202 success",
			statusCodes:  []int{202},
			wantErr:      false,
			wantAttempts: 1,
		},
		{
			name:         "204 success",
			statusCodes:  []int{204},
			wantErr:      false,
			wantAttempts: 1,
		},
		{
			name:         "429 retries then errors",
			statusCodes:  []int{429, 429, 429},
			wantErr:      true,
			wantAttempts: 3,
		},
		{
			name:         "503 retries then errors",
			statusCodes:  []int{503, 503, 503},
			wantErr:      true,
			wantAttempts: 3,
		},
		{
			name:         "500 retries then errors",
			statusCodes:  []int{500, 500, 500},
			wantErr:      true,
			wantAttempts: 3,
		},
		{
			name:         "400 no retry immediate error",
			statusCodes:  []int{400},
			wantErr:      true,
			wantErrMsg:   "400",
			wantAttempts: 1,
		},
		{
			name:         "401 no retry immediate error",
			statusCodes:  []int{401},
			wantErr:      true,
			wantErrMsg:   "401",
			wantAttempts: 1,
		},
		{
			name:         "403 no retry immediate error",
			statusCodes:  []int{403},
			wantErr:      true,
			wantAttempts: 1,
		},
		{
			name:         "500 succeeds on second attempt",
			statusCodes:  []int{500, 200},
			wantErr:      false,
			wantAttempts: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var attempts int32
			idx := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&attempts, 1)
				code := tc.statusCodes[idx]
				if idx < len(tc.statusCodes)-1 {
					idx++
				}
				w.WriteHeader(code)
			}))
			defer srv.Close()

			client := srv.Client()
			err := post(context.Background(), client, srv.URL, nil, []byte(`{}`))

			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if tc.wantErrMsg != "" && err != nil && !strings.Contains(err.Error(), tc.wantErrMsg) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrMsg)
			}
			got := int(atomic.LoadInt32(&attempts))
			if got != tc.wantAttempts {
				t.Errorf("attempts = %d, want %d", got, tc.wantAttempts)
			}
		})
	}
}

func TestPost_AuthorizationHeader(t *testing.T) {
	origSleep := sleepFn
	t.Cleanup(func() { sleepFn = origSleep })
	sleepFn = func(time.Duration) {}

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	headers := map[string]string{"Authorization": "Bearer test-token"}
	err := post(context.Background(), srv.Client(), srv.URL, headers, []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-token")
	}
}

func TestPost_ContentTypeHeader(t *testing.T) {
	origSleep := sleepFn
	t.Cleanup(func() { sleepFn = origSleep })
	sleepFn = func(time.Duration) {}

	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := post(context.Background(), srv.Client(), srv.URL, nil, []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
}

func TestPost_ContextCancellation(t *testing.T) {
	origSleep := sleepFn
	t.Cleanup(func() { sleepFn = origSleep })

	// Use real sleep but cancel context — the select in post should return ctx.Err().
	// We make the first attempt fail with 503 to trigger a retry with delay.
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Replace sleepFn so it cancels the context, simulating cancellation during backoff.
	sleepFn = func(d time.Duration) {
		cancel()
	}

	err := post(ctx, srv.Client(), srv.URL, nil, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error from context cancellation, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
