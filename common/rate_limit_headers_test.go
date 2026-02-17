package common

import (
	"net/http"
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
