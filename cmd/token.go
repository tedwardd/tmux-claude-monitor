package cmd

import (
	"errors"

	"claude-monitor/internal/api"
)

// tokenSource holds the bearer token the daemon polls with and reloads it when
// the endpoint rejects it.
//
// The daemon used to read credentials once at startup and keep them for its
// whole life. Claude Code rotates the token, so once the startup copy expired
// every fetch failed with no way back except restarting the daemon, and on macOS
// that meant the status bar sat at ?? until someone noticed.
//
// fetch and reload are fields so the retry path can be tested without a network
// or a keychain.
type tokenSource struct {
	path   string
	token  string
	fetch  func(token string) (api.UsageData, error)
	reload func(path string) (api.Credentials, error)
}

func newTokenSource(path, token string) *tokenSource {
	return &tokenSource{
		path:   path,
		token:  token,
		fetch:  api.FetchUsage,
		reload: api.LoadCredentials,
	}
}

// get fetches usage, reloading credentials and retrying once when the token was
// rejected and the stored material has since changed. It deliberately does not
// retry on an unchanged token: that would just be rejected again, and this
// endpoint rate-limits.
func (s *tokenSource) get() (api.UsageData, error) {
	usage, err := s.fetch(s.token)

	var authErr *api.AuthError
	if !errors.As(err, &authErr) {
		return usage, err
	}

	fresh, reloadErr := s.reload(s.path)
	if reloadErr != nil || fresh.AccessToken == "" || fresh.AccessToken == s.token {
		return usage, err
	}

	s.token = fresh.AccessToken
	return s.fetch(s.token)
}
