package scanner

import (
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

	value, _, err := gitlabCallWithRateLimitRetry("test-call", func() (int, *gitlab.Response, error) {
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
