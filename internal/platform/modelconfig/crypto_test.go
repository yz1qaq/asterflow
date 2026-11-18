package modelconfig

import (
	"bytes"
	"strings"
	"testing"
)

func TestCredentialCipher(t *testing.T) {
	if _, err := NewCipher(nil); err == nil {
		t.Fatal("empty key accepted")
	}
	c, err := NewCipher([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"", "demo-api-key", "credential-密钥"} {
		encrypted, err := c.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}
		again, err := c.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(encrypted, again) {
			t.Fatal("nonce reused for same plaintext")
		}
		result, err := c.Decrypt(encrypted)
		if err != nil || result != plain {
			t.Fatalf("round trip failed: %v", err)
		}
		other, err := NewCipher([]byte(strings.Repeat("b", 32)))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := other.Decrypt(encrypted); err == nil || got != "" {
			t.Fatal("wrong key returned plaintext")
		}
		tampered := append([]byte(nil), encrypted...)
		tampered[len(tampered)-1] ^= 1
		if got, err := c.Decrypt(tampered); err == nil || got != "" {
			t.Fatal("tampered credential accepted")
		}
		tampered = append([]byte(nil), encrypted...)
		tampered[0] ^= 1
		if got, err := c.Decrypt(tampered); err == nil || got != "" {
			t.Fatal("tampered nonce accepted")
		}
	}
	for n := 0; n < c.aead.NonceSize()+c.aead.Overhead(); n++ {
		if got, err := c.Decrypt(make([]byte, n)); err == nil || got != "" {
			t.Fatalf("short ciphertext accepted: %d", n)
		}
	}
}
