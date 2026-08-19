package secure

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func mustKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func TestAESGCMEncryptor_RoundTrip(t *testing.T) {
	enc, err := NewAESGCMEncryptor(mustKey(t))
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor: %v", err)
	}

	plaintext := []byte(`{"access_token":"super-secret"}`)
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ciphertext, plaintext) {
		t.Fatal("ciphertext contains the plaintext verbatim")
	}

	got, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt = %q, want %q", got, plaintext)
	}
}

func TestAESGCMEncryptor_NoncesNeverRepeat(t *testing.T) {
	enc, err := NewAESGCMEncryptor(mustKey(t))
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor: %v", err)
	}

	plaintext := []byte("same plaintext every time")
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		ciphertext, err := enc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		if seen[string(ciphertext)] {
			t.Fatalf("nonce/ciphertext repeated at iteration %d", i)
		}
		seen[string(ciphertext)] = true
	}
}

func TestAESGCMEncryptor_TamperedCiphertextFailsToDecrypt(t *testing.T) {
	enc, err := NewAESGCMEncryptor(mustKey(t))
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor: %v", err)
	}

	ciphertext, err := enc.Encrypt([]byte("integrity matters"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	tampered := append([]byte{}, ciphertext...)
	tampered[len(tampered)-1] ^= 0xFF

	if _, err := enc.Decrypt(tampered); err == nil {
		t.Fatal("Decrypt of tampered ciphertext succeeded, want error")
	}
}

func TestAESGCMEncryptor_WrongKeyFailsToDecrypt(t *testing.T) {
	enc1, err := NewAESGCMEncryptor(mustKey(t))
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor: %v", err)
	}
	enc2, err := NewAESGCMEncryptor(mustKey(t))
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor: %v", err)
	}

	ciphertext, err := enc1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := enc2.Decrypt(ciphertext); err == nil {
		t.Fatal("Decrypt with the wrong key succeeded, want error")
	}
}

func TestNewAESGCMEncryptor_RejectsWrongKeySize(t *testing.T) {
	for _, n := range []int{0, 16, 24, 31, 33, 64} {
		if _, err := NewAESGCMEncryptor(make([]byte, n)); err != ErrInvalidKeySize {
			t.Errorf("key size %d: err = %v, want ErrInvalidKeySize", n, err)
		}
	}
}

func TestAESGCMEncryptor_DecryptRejectsShortCiphertext(t *testing.T) {
	enc, err := NewAESGCMEncryptor(mustKey(t))
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor: %v", err)
	}
	if _, err := enc.Decrypt([]byte("short")); err != ErrCiphertextTooShort {
		t.Fatalf("Decrypt short ciphertext: err = %v, want ErrCiphertextTooShort", err)
	}
}
