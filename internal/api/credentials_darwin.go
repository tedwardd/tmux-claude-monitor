//go:build darwin

package api

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"strings"
)

// keychainService is the generic-password service name Claude Code stores its
// OAuth credentials under in the macOS login keychain.
const keychainService = "Claude Code-credentials"

// secretStoreCredentials reads the token from the login keychain. The stored
// secret is the same JSON document Claude Code writes to disk on other
// platforms.
func secretStoreCredentials() (Credentials, error) {
	queries := [][]string{}
	if u, err := user.Current(); err == nil && u.Username != "" {
		queries = append(queries, []string{"find-generic-password", "-s", keychainService, "-a", u.Username, "-w"})
	}
	queries = append(queries, []string{"find-generic-password", "-s", keychainService, "-w"})

	var lastErr error
	for _, q := range queries {
		out, err := exec.Command("security", q...).Output()
		if err != nil {
			lastErr = keychainError(err)
			continue
		}
		secret, err := decodeKeychainSecret(out)
		if err != nil {
			return Credentials{}, fmt.Errorf("decode keychain secret: %w", err)
		}
		return parseCredentials(secret)
	}
	return Credentials{}, fmt.Errorf("keychain item %q unreadable: %w", keychainService, lastErr)
}

func keychainError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return err
}

// decodeKeychainSecret undoes the hex encoding `security -w` falls back to when
// the stored secret is not printable.
func decodeKeychainSecret(out []byte) ([]byte, error) {
	s := strings.TrimSpace(string(out))
	if !strings.HasPrefix(s, "0x") {
		return []byte(s), nil
	}
	fields := strings.Fields(strings.TrimPrefix(s, "0x"))
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty hex payload")
	}
	return hex.DecodeString(fields[0])
}
