package crawler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPubliccodeGetStatusWithRetryCanceledContextSkipsRequest(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	statusCode, err := publiccodeGetStatusWithRetry(ctx, server.URL, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("publiccodeGetStatusWithRetry error = %v, want %v", err, context.Canceled)
	}

	if statusCode != 0 {
		t.Fatalf("publiccodeGetStatusWithRetry status = %d, want 0", statusCode)
	}

	if calls.Load() != 0 {
		t.Fatalf("publiccodeGetStatusWithRetry performed %d requests, want 0", calls.Load())
	}
}

func TestPubliccodeGetStatusWithRetryRespectsDeadlineDuringRateLimitWait(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	statusCode, err := publiccodeGetStatusWithRetry(ctx, server.URL, nil)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("publiccodeGetStatusWithRetry error = %v, want %v", err, context.DeadlineExceeded)
	}

	if statusCode != http.StatusTooManyRequests {
		t.Fatalf("publiccodeGetStatusWithRetry status = %d, want %d", statusCode, http.StatusTooManyRequests)
	}

	if elapsed > time.Second {
		t.Fatalf("publiccodeGetStatusWithRetry took %s, want under %s", elapsed, time.Second)
	}

	if calls.Load() != 1 {
		t.Fatalf("publiccodeGetStatusWithRetry performed %d requests, want 1", calls.Load())
	}
}
