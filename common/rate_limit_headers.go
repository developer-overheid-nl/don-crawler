package common

import (
	"net/http"
	"strconv"
	"time"
)

// RateLimitResetFromHeaders returns the next rate-limit reset moment from
// supported headers. The bool return indicates whether a reset moment could be
// parsed.
func RateLimitResetFromHeaders(headers http.Header) (time.Time, bool) {
	if headers == nil {
		return time.Time{}, false
	}

	for _, key := range []string{"RateLimit-Reset", "X-RateLimit-Reset"} {
		if raw := headers.Get(key); raw != "" {
			if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return time.Unix(unix, 0), true
			}
		}
	}

	if value := headers.Get("Retry-After"); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			return time.Now().Add(time.Duration(seconds) * time.Second), true
		}

		if when, err := http.ParseTime(value); err == nil {
			return when, true
		}
	}

	return time.Time{}, false
}
