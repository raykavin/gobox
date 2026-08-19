// Package secure provides authenticated encryption for opaque byte payloads.
//
// # Usage
//
//	enc, err := secure.NewAESGCMEncryptor(key) // key must be 32 bytes (AES-256)
//	if err != nil {
//	    return err
//	}
//
//	ciphertext, err := enc.Encrypt([]byte(`{"access_token":"super-secret"}`))
//	if err != nil {
//	    return err
//	}
//
//	plaintext, err := enc.Decrypt(ciphertext)
//	if err != nil {
//	    return err
//	}
//
// AESGCMEncryptor implements the Encryptor interface with AES-256-GCM. Encrypt
// draws a fresh random nonce from crypto/rand for every call and prepends it
// to the returned ciphertext, so Decrypt can recover it without any side
// channel. Reusing a nonce with the same key breaks both confidentiality and
// authenticity, so callers must never construct ciphertext by hand and should
// always go through Encrypt.
//
// The encryption key is supplied by the caller and is never derived, stored,
// or logged by this package. Key management (generation, rotation, storage)
// is the caller's responsibility.
package secure
