package vpn

import (
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/curve25519"
)

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privBytes, err := base64.StdEncoding.DecodeString(priv)
	if err != nil || len(privBytes) != 32 {
		t.Fatalf("priv key: %v len=%d", err, len(privBytes))
	}
	pubBytes, err := base64.StdEncoding.DecodeString(pub)
	if err != nil || len(pubBytes) != 32 {
		t.Fatalf("pub key: %v len=%d", err, len(pubBytes))
	}
	derived, err := curve25519.X25519(privBytes, curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	if string(derived) != string(pubBytes) {
		t.Fatal("public key does not match private key")
	}
	if priv == pub {
		t.Fatal("keys must differ")
	}
}
