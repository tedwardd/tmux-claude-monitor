//go:build darwin

package api

import "testing"

func TestDecodeKeychainSecretPlaintext(t *testing.T) {
	got, err := decodeKeychainSecret([]byte(testCredsJSON + "\n"))
	if err != nil {
		t.Fatalf("decodeKeychainSecret: %v", err)
	}
	if string(got) != testCredsJSON {
		t.Errorf("got %q, want %q", got, testCredsJSON)
	}
}

// security falls back to hex when the stored secret is not printable.
func TestDecodeKeychainSecretHex(t *testing.T) {
	got, err := decodeKeychainSecret([]byte("0x7B7D  {}\n"))
	if err != nil {
		t.Fatalf("decodeKeychainSecret: %v", err)
	}
	if string(got) != "{}" {
		t.Errorf("got %q, want %q", got, "{}")
	}
}

func TestDecodeKeychainSecretEmptyHex(t *testing.T) {
	if _, err := decodeKeychainSecret([]byte("0x")); err == nil {
		t.Error("expected error for empty hex payload")
	}
}
