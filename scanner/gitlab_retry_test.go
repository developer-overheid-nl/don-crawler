package scanner

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestGitlabRateLimitResetWithoutHeadersStillSignalsRateLimit(t *testing.T) {
	resp := &gitlab.Response{
		Response: &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{},
		},
	}

	reset, ok := gitlabRateLimitReset(resp, errors.New("rate limited"))
	if !ok {
		t.Fatal("gitlabRateLimitReset should detect 429 as rate limited")
	}

	if !reset.IsZero() {
		t.Fatalf("gitlabRateLimitReset reset = %s, want zero time when no headers are present", reset)
	}
}

func TestGitlabCallWithRateLimitRetryRetriesOn429(t *testing.T) {
	previousFallback := gitLabRateLimitFallbackWait
	gitLabRateLimitFallbackWait = time.Millisecond
	defer func() { gitLabRateLimitFallbackWait = previousFallback }()

	attempts := 0

	value, _, err := gitlabCallWithRateLimitRetry(context.Background(), "test-call", func() (int, *gitlab.Response, error) {
		attempts++

		if attempts == 1 {
			return 0, &gitlab.Response{
				Response: &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{},
				},
			}, errors.New("rate limited")
		}

		return 42, nil, nil
	})
	if err != nil {
		t.Fatalf("gitlabCallWithRateLimitRetry returned error: %v", err)
	}

	if value != 42 {
		t.Fatalf("gitlabCallWithRateLimitRetry value = %d, want 42", value)
	}

	if attempts != 2 {
		t.Fatalf("gitlabCallWithRateLimitRetry attempts = %d, want 2", attempts)
	}
}

func TestGitlabCallWithRateLimitRetryReturnsErrorAfterMaxRetries(t *testing.T) {
	previousFallback := gitLabRateLimitFallbackWait
	gitLabRateLimitFallbackWait = time.Millisecond
	defer func() { gitLabRateLimitFallbackWait = previousFallback }()

	attempts := 0

	_, _, err := gitlabCallWithRateLimitRetry(context.Background(), "test-call", func() (int, *gitlab.Response, error) {
		attempts++
		return 0, &gitlab.Response{
			Response: &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{},
			},
		}, errors.New("rate limited")
	})

	if err == nil {
		t.Fatal("gitlabCallWithRateLimitRetry should return error after max retries")
	}

	var rateLimitErr RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("gitlabCallWithRateLimitRetry error = %T, want RateLimitError", err)
	}

	if rateLimitErr.Provider != "GitLab" {
		t.Fatalf("RateLimitError.Provider = %s, want GitLab", rateLimitErr.Provider)
	}

	// Should attempt initial call + maxGitLabRateLimitRetries
	expectedAttempts := maxGitLabRateLimitRetries + 1
	if attempts != expectedAttempts {
		t.Fatalf("gitlabCallWithRateLimitRetry attempts = %d, want %d", attempts, expectedAttempts)
	}
}

func TestGitlabCallWithRateLimitRetryRespectsContextCancellation(t *testing.T) {
	previousFallback := gitLabRateLimitFallbackWait
	gitLabRateLimitFallbackWait = 100 * time.Millisecond
	defer func() { gitLabRateLimitFallbackWait = previousFallback }()

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	// Cancel context after first retry wait starts
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, _, err := gitlabCallWithRateLimitRetry(ctx, "test-call", func() (int, *gitlab.Response, error) {
		attempts++
		return 0, &gitlab.Response{
			Response: &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{},
			},
		}, errors.New("rate limited")
	})

	if err == nil {
		t.Fatal("gitlabCallWithRateLimitRetry should return error when context is cancelled")
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("gitlabCallWithRateLimitRetry error = %v, want context.Canceled", err)
	}

	// Should only attempt once before context is cancelled during wait
	if attempts != 1 {
		t.Fatalf("gitlabCallWithRateLimitRetry attempts = %d, want 1", attempts)
	}
}
