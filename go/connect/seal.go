package connect

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// Sealer keeps tokens unreadable at rest. aad binds a sealed token to its
// row, so one copied onto another row does not open.
type Sealer interface {
	Seal(plain, aad string) ([]byte, error)
	Open(sealed []byte, aad string) (string, error)
}

// AESGCM seals with AES-256-GCM, the nonce ahead of the ciphertext.
type AESGCM struct {
	aead cipher.AEAD
}

var ErrBadSealKey = errors.New("connect seal key must be 32 bytes, base64")

func NewAESGCM(key string) (*AESGCM, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 {
		return nil, ErrBadSealKey
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCM{aead: aead}, nil
}

func (s *AESGCM) Seal(plain, aad string) ([]byte, error) {
	if plain == "" {
		return nil, nil
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, []byte(plain), []byte(aad)), nil
}

func (s *AESGCM) Open(sealed []byte, aad string) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return "", fmt.Errorf("sealed token too short")
	}
	plain, err := s.aead.Open(nil, sealed[:n], sealed[n:], []byte(aad))
	if err != nil {
		return "", fmt.Errorf("open sealed token: %w", err)
	}
	return string(plain), nil
}
