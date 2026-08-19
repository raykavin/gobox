package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// KeySize is the required key length for NewAESGCMEncryptor: 32 bytes for
// AES-256.
const KeySize = 32

var (
	// ErrInvalidKeySize is returned by NewAESGCMEncryptor when the key is
	// not exactly KeySize bytes.
	ErrInvalidKeySize = fmt.Errorf("secure: key must be %d bytes (AES-256)", KeySize)

	// ErrCiphertextTooShort is returned by Decrypt when the input is too
	// short to even contain a nonce, so it cannot be this encryptor's own
	// output.
	ErrCiphertextTooShort = errors.New("secure: ciphertext shorter than nonce size")
)

// Encryptor authenticates and encrypts/decrypts opaque byte payloads.
// Implementations must use a fresh, unpredictable nonce for every Encrypt
// call with the same key reusing a nonce with AES-GCM breaks both
// confidentiality and authenticity of every message that shares it.
type Encryptor interface {
	Encrypt(plaintext []byte) (ciphertext []byte, err error)
	Decrypt(ciphertext []byte) (plaintext []byte, err error)
}

// AESGCMEncryptor implements Encryptor with AES-256-GCM. Safe for concurrent
// use.
type AESGCMEncryptor struct {
	aead cipher.AEAD
}

var _ Encryptor = (*AESGCMEncryptor)(nil)

// NewAESGCMEncryptor builds an AESGCMEncryptor from a 32-byte key. The key
// must come from the caller's own secure configuration; it is never derived,
// stored, or logged by this package.
func NewAESGCMEncryptor(key []byte) (*AESGCMEncryptor, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secure: build AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secure: build GCM: %w", err)
	}

	return &AESGCMEncryptor{aead: aead}, nil
}

// Encrypt seals plaintext with a fresh random nonce (crypto/rand), prepended
// to the returned ciphertext so Decrypt can recover it. Never reuses a nonce
// for a given key: each call draws a new one.
func (e *AESGCMEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secure: generate nonce: %w", err)
	}
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt authenticates and opens ciphertext previously produced by Encrypt
// (nonce-prefixed). Returns an error if the nonce is missing/too short or
// authentication fails (tampered ciphertext, or the wrong key).
func (e *AESGCMEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := e.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrCiphertextTooShort
	}

	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("secure: decrypt: %w", err)
	}
	return plaintext, nil
}
