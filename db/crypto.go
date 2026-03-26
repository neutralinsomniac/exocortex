package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

// EncryptPayload encrypts plaintext with AES-256-GCM using a key derived from
// token. Returns nonce || ciphertext.
func EncryptPayload(token string, plaintext []byte) ([]byte, error) {
	key := sha256.Sum256([]byte(token))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("gen nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptPayload decrypts a nonce || ciphertext blob produced by EncryptPayload.
func DecryptPayload(token string, data []byte) ([]byte, error) {
	key := sha256.Sum256([]byte(token))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, data[:ns], data[ns:], nil)
}
