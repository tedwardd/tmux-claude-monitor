package api

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCredsJSON = `{"claudeAiOauth":{"accessToken":"sk-file-token"}}`

// stubSecretStore swaps the platform secret store for the duration of a test.
func stubSecretStore(t *testing.T, fn func() (Credentials, error)) {
	t.Helper()
	orig := secretStore
	secretStore = fn
	t.Cleanup(func() { secretStore = orig })
}

func writeCreds(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".credentials.json")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	return p
}

func TestLoadCredentialsPrefersFile(t *testing.T) {
	stubSecretStore(t, func() (Credentials, error) {
		t.Error("secret store consulted despite a readable credentials file")
		return Credentials{}, nil
	})

	got, err := LoadCredentials(writeCreds(t, testCredsJSON))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got.AccessToken != "sk-file-token" {
		t.Errorf("AccessToken: got %q, want %q", got.AccessToken, "sk-file-token")
	}
}

func TestLoadCredentialsMalformedFileDoesNotFallBack(t *testing.T) {
	stubSecretStore(t, func() (Credentials, error) {
		t.Error("secret store consulted for a malformed credentials file")
		return Credentials{AccessToken: "sk-store-token"}, nil
	})

	if _, err := LoadCredentials(writeCreds(t, "not json")); err == nil {
		t.Fatal("expected error for malformed credentials file")
	}
}

func TestLoadCredentialsFallsBackToSecretStore(t *testing.T) {
	stubSecretStore(t, func() (Credentials, error) {
		return Credentials{AccessToken: "sk-store-token"}, nil
	})

	got, err := LoadCredentials(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got.AccessToken != "sk-store-token" {
		t.Errorf("AccessToken: got %q, want %q", got.AccessToken, "sk-store-token")
	}
}

func TestLoadCredentialsNoStoreReportsFileError(t *testing.T) {
	stubSecretStore(t, func() (Credentials, error) {
		return Credentials{}, errNoSecretStore
	})

	missing := filepath.Join(t.TempDir(), "absent.json")
	_, err := LoadCredentials(missing)
	if err == nil {
		t.Fatal("expected error when neither source has credentials")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error should name the missing file, got %q", err)
	}
	if strings.Contains(err.Error(), "no platform secret store") {
		t.Errorf("error should not leak the sentinel, got %q", err)
	}
}

func TestLoadCredentialsReportsBothFailures(t *testing.T) {
	stubSecretStore(t, func() (Credentials, error) {
		return Credentials{}, errors.New("keychain item unreadable")
	})

	missing := filepath.Join(t.TempDir(), "absent.json")
	_, err := LoadCredentials(missing)
	if err == nil {
		t.Fatal("expected error when neither source has credentials")
	}
	for _, want := range []string{missing, "keychain item unreadable"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}
