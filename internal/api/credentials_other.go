//go:build !darwin

package api

// secretStoreCredentials reports that there is no OS credential store to read.
// Claude Code writes a credentials file on these platforms.
func secretStoreCredentials() (Credentials, error) {
	return Credentials{}, errNoSecretStore
}
