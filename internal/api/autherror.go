package api

import "fmt"

// AuthError reports that the endpoint rejected the token. It is worth
// distinguishing from other failures because it is the one error a reload can
// fix: the daemon reads the token once at startup and Claude Code rotates it,
// so a rejection often just means the copy in memory is stale.
type AuthError struct {
	StatusCode int
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("usage rejected the token (HTTP %d)", e.StatusCode)
}
