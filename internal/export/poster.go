package export

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// sleepFn is a package-level variable so tests can replace it to avoid real delays.
var sleepFn = time.Sleep

// post sends body to endpoint with given headers via HTTP POST.
// Retries up to 3 total attempts on 429 and 5xx responses.
// Returns nil on 200, 202, 204. Returns error on other status codes or network failure.
func post(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, body []byte) error {
	delays := []time.Duration{0, time.Second, 2 * time.Second}
	var lastErr error
	for attempt, delay := range delays {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			sleepFn(delay)
			// Check again after sleep (sleepFn may have triggered cancellation).
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		lastErr = doPost(ctx, client, endpoint, headers, body)
		if lastErr == nil {
			return nil
		}
		// Check if retryable.
		if he, ok := lastErr.(*httpError); ok {
			if !isRetryable(he.statusCode) {
				return lastErr
			}
		}
		// Network errors are retried.
	}
	return fmt.Errorf("post: %d attempts failed: %w", len(delays), lastErr)
}

type httpError struct {
	statusCode int
	body       string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.statusCode, e.body)
}

func isRetryable(code int) bool {
	return code == 429 || code >= 500
}

func doPost(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200, 202, 204:
		return nil
	default:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &httpError{statusCode: resp.StatusCode, body: string(b)}
	}
}
