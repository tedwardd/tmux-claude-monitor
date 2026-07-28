package cmd

import (
	"errors"
	"testing"

	"claude-monitor/internal/api"
)

// stubSource builds a tokenSource whose fetch returns the queued results in
// order, recording the token each call was made with.
func stubSource(t *testing.T, token string, results []error, reload func(string) (api.Credentials, error)) (*tokenSource, *[]string) {
	t.Helper()
	var used []string
	i := 0
	return &tokenSource{
		path:  "/nonexistent/creds.json",
		token: token,
		fetch: func(tok string) (api.UsageData, error) {
			used = append(used, tok)
			if i >= len(results) {
				t.Fatalf("fetch called %d times, only %d results queued", i+1, len(results))
			}
			err := results[i]
			i++
			return api.UsageData{SessionUtilization: 7}, err
		},
		reload: reload,
	}, &used
}

func TestTokenSourcePassesThroughSuccess(t *testing.T) {
	s, used := stubSource(t, "tok-1", []error{nil}, func(string) (api.Credentials, error) {
		t.Error("reload must not run when the fetch succeeded")
		return api.Credentials{}, nil
	})

	usage, err := s.get()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if usage.SessionUtilization != 7 {
		t.Errorf("SessionUtilization = %v, want 7", usage.SessionUtilization)
	}
	if len(*used) != 1 {
		t.Errorf("fetched %d times, want 1", len(*used))
	}
}

// A rejected token is the one failure a reload can fix, so a rotated token must
// be picked up without restarting the daemon.
func TestTokenSourceReloadsAndRetriesOnAuthFailure(t *testing.T) {
	s, used := stubSource(t, "stale",
		[]error{&api.AuthError{StatusCode: 401}, nil},
		func(string) (api.Credentials, error) {
			return api.Credentials{AccessToken: "rotated"}, nil
		})

	if _, err := s.get(); err != nil {
		t.Fatalf("get should have recovered: %v", err)
	}
	if want := []string{"stale", "rotated"}; len(*used) != 2 || (*used)[0] != want[0] || (*used)[1] != want[1] {
		t.Errorf("tokens used = %v, want %v", *used, want)
	}
	if s.token != "rotated" {
		t.Errorf("source kept %q, want the rotated token", s.token)
	}
}

// Retrying an unchanged token would be rejected again, and this endpoint
// rate-limits, so it must not spend a second request on it.
func TestTokenSourceDoesNotRetryUnchangedToken(t *testing.T) {
	s, used := stubSource(t, "same",
		[]error{&api.AuthError{StatusCode: 401}},
		func(string) (api.Credentials, error) {
			return api.Credentials{AccessToken: "same"}, nil
		})

	var authErr *api.AuthError
	if _, err := s.get(); !errors.As(err, &authErr) {
		t.Fatalf("expected the auth error to surface, got %v", err)
	}
	if len(*used) != 1 {
		t.Errorf("fetched %d times, want 1: an unchanged token must not be retried", len(*used))
	}
}

func TestTokenSourceSurvivesReloadFailure(t *testing.T) {
	s, used := stubSource(t, "stale",
		[]error{&api.AuthError{StatusCode: 403}},
		func(string) (api.Credentials, error) {
			return api.Credentials{}, errors.New("keychain unavailable")
		})

	var authErr *api.AuthError
	if _, err := s.get(); !errors.As(err, &authErr) {
		t.Fatalf("expected the original auth error, got %v", err)
	}
	if len(*used) != 1 {
		t.Errorf("fetched %d times, want 1", len(*used))
	}
	if s.token != "stale" {
		t.Errorf("token changed to %q despite a failed reload", s.token)
	}
}

// An empty token would turn every later fetch into a guaranteed rejection.
func TestTokenSourceIgnoresEmptyReloadedToken(t *testing.T) {
	s, _ := stubSource(t, "stale",
		[]error{&api.AuthError{StatusCode: 401}},
		func(string) (api.Credentials, error) {
			return api.Credentials{AccessToken: ""}, nil
		})

	if _, err := s.get(); err == nil {
		t.Fatal("expected the auth error to surface")
	}
	if s.token != "stale" {
		t.Errorf("token replaced with %q", s.token)
	}
}

// Non-auth failures must not trigger a reload; a 429 or a timeout says nothing
// about the token.
func TestTokenSourceLeavesOtherErrorsAlone(t *testing.T) {
	for _, e := range []error{
		&api.RateLimitError{RetryAfter: 0},
		errors.New("usage request: context deadline exceeded"),
	} {
		s, used := stubSource(t, "tok", []error{e}, func(string) (api.Credentials, error) {
			t.Errorf("reload ran for %v", e)
			return api.Credentials{}, nil
		})
		if _, err := s.get(); err == nil {
			t.Errorf("expected %v to surface", e)
		}
		if len(*used) != 1 {
			t.Errorf("fetched %d times for %v, want 1", len(*used), e)
		}
	}
}
