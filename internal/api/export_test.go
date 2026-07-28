package api

import "time"

// ParseRetryAfterForTest exposes the header parser to the external test package.
func ParseRetryAfterForTest(header string) time.Duration { return parseRetryAfter(header) }
