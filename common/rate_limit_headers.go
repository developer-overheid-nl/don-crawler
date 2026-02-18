package common

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// Protect crawler workers from extremely large or malicious retry hints.
	maxRateLimitResetDelay = 24 * time.Hour
	maxRetryAfterSeconds   = int64(maxRateLimitResetDelay / time.Second)
)

// RateLimitResetFromHeaders returns the next rate-limit reset moment from
// supported headers. The bool return indicates whether a reset moment could be
// parsed.
func RateLimitResetFromHeaders(headers http.Header) (time.Time, bool) {
	if headers == nil {
		return time.Time{}, false
	}

	now := time.Now()
	maxAcceptedReset := now.Add(maxRateLimitResetDelay)
	latestReset := time.Time{}

	consider := func(candidate time.Time) {
		if candidate.IsZero() || candidate.After(maxAcceptedReset) {
			return
		}

		if latestReset.IsZero() || candidate.After(latestReset) {
			latestReset = candidate
		}
	}

	for _, key := range []string{"RateLimit-Reset", "X-RateLimit-Reset"} {
		for _, raw := range headers.Values(key) {
			for _, value := range strings.Split(raw, ",") {
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}

				if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
					consider(time.Unix(unix, 0))
				}
			}
		}
	}

	for _, raw := range headers.Values("Retry-After") {
		if reset, ok := retryAfterReset(raw, now); ok {
			consider(reset)
		}
	}

	if latestReset.IsZero() {
		return time.Time{}, false
	}

	return latestReset, true
}

func retryAfterReset(raw string, now time.Time) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 || seconds > maxRetryAfterSeconds {
			return time.Time{}, false
		}

		return now.Add(time.Duration(seconds) * time.Second), true
	}

	when, err := http.ParseTime(value)
	if err != nil {
		return time.Time{}, false
	}

	return when, true
}
