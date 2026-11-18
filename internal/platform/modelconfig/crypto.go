package modelconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(secret []byte) (*Cipher, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("empty credential encryption key")
	}

	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	return &Cipher{aead: aead}, nil

}

func (c *Cipher) Encrypt(plain string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate credential nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, []byte(plain), nil), nil
}

func (c *Cipher) Decrypt(ciphertext []byte) (string, error) {
	n := c.aead.NonceSize()
	if len(ciphertext) < n+c.aead.Overhead() {
		return "", fmt.Errorf("invalid credential ciphertext")
	}

	plain, err := c.aead.Open(
		nil,
		ciphertext[:n],
		ciphertext[n:],
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("decrypt API key: %w", err)
	}
	return string(plain), nil
}
