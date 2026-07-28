package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// The usage endpoint is shared by every client holding the same OAuth token, so
// a 429 is common when more than one usage monitor is running. Honour what the
// server asks for rather than retrying on our own schedule and re-tripping it.
const (
	minRetryAfter = 1 * time.Second
	maxRetryAfter = 1 * time.Hour
)

// RateLimitError reports a 429 along with how long the server asked us to wait.
// RetryAfter is zero when the response carried no usable hint.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("usage rate-limited (HTTP 429), retry after %s", e.RetryAfter)
	}
	return "usage rate-limited (HTTP 429)"
}

// parseRetryAfter reads the header in either of its forms, delay-seconds or an
// HTTP date, and clamps the result. A hostile or mistaken value should not stall
// polling indefinitely, and Ticker.Reset panics on a non-positive interval.
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		return clampRetryAfter(time.Duration(secs) * time.Second)
	}
	if when, err := http.ParseTime(header); err == nil {
		return clampRetryAfter(time.Until(when))
	}
	return 0
}

func clampRetryAfter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if d < minRetryAfter {
		return minRetryAfter
	}
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}
