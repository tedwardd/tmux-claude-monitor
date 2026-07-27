package api

import (
	"errors"
	"fmt"
	"io/fs"
)

// errNoSecretStore marks platforms with no OS credential store worth consulting.
var errNoSecretStore = errors.New("no platform secret store")

// secretStore is a seam so tests never reach the real keychain.
var secretStore = secretStoreCredentials

// LoadCredentials resolves the Claude Code OAuth token, preferring the
// credentials file and falling back to the platform secret store. Claude Code
// writes a file on Linux but keeps the token in the login keychain on macOS.
func LoadCredentials(path string) (Credentials, error) {
	fileErr := errors.New("no credentials path configured")
	if path != "" {
		creds, err := ReadCredentials(path)
		if err == nil {
			return creds, nil
		}
		// A file that exists but is unreadable or malformed is a real error;
		// only a missing file should fall through to the secret store.
		if !errors.Is(err, fs.ErrNotExist) {
			return Credentials{}, err
		}
		fileErr = err
	}

	creds, storeErr := secretStore()
	if storeErr == nil {
		return creds, nil
	}
	if errors.Is(storeErr, errNoSecretStore) {
		return Credentials{}, fileErr
	}
	return Credentials{}, fmt.Errorf("%v; %w", fileErr, storeErr)
}
