package crypto

import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	Init("ZGV2LW9ubHktZmFsbGJhY2stZG8tbm90LXVzZS1wcm9k")
	plain := "gitlab-token-xyz"
	enc, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == plain || !strings.HasPrefix(enc, "enc:") {
		t.Fatalf("ciphertext should be prefixed with enc: and differ from plaintext, got %q", enc)
	}
	got, err := Decrypt(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("roundtrip mismatch: got %q want %q", got, plain)
	}
}

func TestDecryptPlaintextPassesThrough(t *testing.T) {
	Init("ZGV2LW9ubHktZmFsbGJhY2stZG8tbm90LXVzZS1wcm9k")
	got, err := Decrypt("legacy-plaintext")
	if err != nil {
		t.Fatalf("decrypt plaintext: %v", err)
	}
	if got != "legacy-plaintext" {
		t.Fatalf("expected plaintext passthrough, got %q", got)
	}
}

func TestEncryptEmptyReturnsEmpty(t *testing.T) {
	Init("ZGV2LW9ubHktZmFsbGJhY2stZG8tbm90LXVzZS1wcm9k")
	if v, _ := Encrypt(""); v != "" {
		t.Fatalf("empty should encrypt to empty, got %q", v)
	}
}
