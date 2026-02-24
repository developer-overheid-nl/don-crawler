package common

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestRateLimitResetFromHeadersRateLimitReset(t *testing.T) {
	headers := make(http.Header)
	headers.Set("RateLimit-Reset", "1700000000")

	reset, ok := RateLimitResetFromHeaders(headers)
	if !ok {
		t.Fatal("RateLimitResetFromHeaders should parse RateLimit-Reset")
	}

	expected := time.Unix(1700000000, 0)
	if !reset.Equal(expected) {
		t.Fatalf("reset = %s, want %s", reset, expected)
	}
}

func TestRateLimitResetFromHeadersXRateLimitReset(t *testing.T) {
	headers := make(http.Header)
	headers.Set("X-RateLimit-Reset", "1700000123")

	reset, ok := RateLimitResetFromHeaders(headers)
	if !ok {
		t.Fatal("RateLimitResetFromHeaders should parse X-RateLimit-Reset")
	}

	expected := time.Unix(1700000123, 0)
	if !reset.Equal(expected) {
		t.Fatalf("reset = %s, want %s", reset, expected)
	}
}

func TestRateLimitResetFromHeadersRateLimitResetMultipleValuesUsesLatest(t *testing.T) {
	headers := make(http.Header)
	headers.Add("RateLimit-Reset", "1700000000")
	headers.Add("RateLimit-Reset", "1700000100")

	reset, ok := RateLimitResetFromHeaders(headers)
	if !ok {
		t.Fatal("RateLimitResetFromHeaders should parse multiple RateLimit-Reset values")
	}

	expected := time.Unix(1700000100, 0)
	if !reset.Equal(expected) {
		t.Fatalf("reset = %s, want %s", reset, expected)
	}
}

func TestRateLimitResetFromHeadersRateLimitResetCommaSeparatedUsesLatest(t *testing.T) {
	headers := make(http.Header)
	headers.Set("RateLimit-Reset", "1700000000, 1700000200")

	reset, ok := RateLimitResetFromHeaders(headers)
	if !ok {
		t.Fatal("RateLimitResetFromHeaders should parse comma-separated RateLimit-Reset values")
	}

	expected := time.Unix(1700000200, 0)
	if !reset.Equal(expected) {
		t.Fatalf("reset = %s, want %s", reset, expected)
	}
}

func TestRateLimitResetFromHeadersRetryAfterSeconds(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Retry-After", "3")
	start := time.Now()

	reset, ok := RateLimitResetFromHeaders(headers)
	if !ok {
		t.Fatal("RateLimitResetFromHeaders should parse Retry-After seconds")
	}

	end := time.Now()
	minReset := start.Add(3*time.Second - 200*time.Millisecond)
	maxReset := end.Add(3*time.Second + 200*time.Millisecond)
	if reset.Before(minReset) || reset.After(maxReset) {
		t.Fatalf("reset = %s, expected between %s and %s", reset, minReset, maxReset)
	}
}

func TestRateLimitResetFromHeadersRetryAfterMultipleValuesUsesLatest(t *testing.T) {
	headers := make(http.Header)
	headers.Add("Retry-After", "1")
	headers.Add("Retry-After", "3")
	start := time.Now()

	reset, ok := RateLimitResetFromHeaders(headers)
	if !ok {
		t.Fatal("RateLimitResetFromHeaders should parse multiple Retry-After values")
	}

	end := time.Now()
	minReset := start.Add(3*time.Second - 200*time.Millisecond)
	maxReset := end.Add(3*time.Second + 200*time.Millisecond)
	if reset.Before(minReset) || reset.After(maxReset) {
		t.Fatalf("reset = %s, expected between %s and %s", reset, minReset, maxReset)
	}
}

func TestRateLimitResetFromHeadersRetryAfterDate(t *testing.T) {
	expected := time.Unix(1700001234, 0).UTC()
	headers := make(http.Header)
	headers.Set("Retry-After", expected.Format(http.TimeFormat))

	reset, ok := RateLimitResetFromHeaders(headers)
	if !ok {
		t.Fatal("RateLimitResetFromHeaders should parse Retry-After HTTP date")
	}

	if !reset.Equal(expected) {
		t.Fatalf("reset = %s, want %s", reset, expected)
	}
}

func TestRateLimitResetFromHeadersRetryAfterNegativeIgnored(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Retry-After", "-1")

	reset, ok := RateLimitResetFromHeaders(headers)
	if ok {
		t.Fatalf("RateLimitResetFromHeaders should ignore negative Retry-After, got reset %s", reset)
	}
}

func TestRateLimitResetFromHeadersRetryAfterTooLargeIgnored(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Retry-After", strconv.FormatInt(maxRetryAfterSeconds+1, 10))

	reset, ok := RateLimitResetFromHeaders(headers)
	if ok {
		t.Fatalf("RateLimitResetFromHeaders should ignore huge Retry-After seconds, got reset %s", reset)
	}
}

func TestRateLimitResetFromHeadersRetryAfterFarFutureDateIgnored(t *testing.T) {
	headers := make(http.Header)
	tooFar := time.Now().Add(maxRateLimitResetDelay + time.Hour).UTC()
	headers.Set("Retry-After", tooFar.Format(http.TimeFormat))

	reset, ok := RateLimitResetFromHeaders(headers)
	if ok {
		t.Fatalf("RateLimitResetFromHeaders should ignore far-future Retry-After date, got reset %s", reset)
	}
}

func TestRateLimitResetFromHeadersInvalidValues(t *testing.T) {
	headers := make(http.Header)
	headers.Set("RateLimit-Reset", "not-a-number")
	headers.Set("X-RateLimit-Reset", "still-not-a-number")
	headers.Set("Retry-After", "not-a-date")

	reset, ok := RateLimitResetFromHeaders(headers)
	if ok {
		t.Fatalf("RateLimitResetFromHeaders should fail for invalid headers, got reset %s", reset)
	}
}
