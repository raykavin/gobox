# secure

The `secure` package provides authenticated encryption for opaque byte payloads. It is intended for shared infrastructure code that needs to encrypt sensitive values (tokens, secrets, PII) at rest without pulling in a larger cryptography framework.

## Import

```go
import "github.com/raykavin/gobox/secure"
```

## What it provides

- `Encryptor` interface for authenticated encrypt/decrypt of byte payloads
- `AESGCMEncryptor`, an `Encryptor` implementation using AES-256-GCM
- automatic nonce generation and management: a fresh random nonce is drawn for every `Encrypt` call and prepended to the ciphertext
- safe for concurrent use

## Main types

- `NewAESGCMEncryptor(key []byte) (*AESGCMEncryptor, error)`: builds an encryptor from a 32-byte AES-256 key
- `(*AESGCMEncryptor).Encrypt(plaintext []byte) ([]byte, error)`: seals `plaintext` with a fresh random nonce
- `(*AESGCMEncryptor).Decrypt(ciphertext []byte) ([]byte, error)`: authenticates and opens ciphertext previously produced by `Encrypt`

## Example

```go
package main

import (
	"log"

	"github.com/raykavin/gobox/secure"
)

func main() {
	key := make([]byte, secure.KeySize) // load a real 32-byte key from your secret store
	enc, err := secure.NewAESGCMEncryptor(key)
	if err != nil {
		log.Fatal(err)
	}

	ciphertext, err := enc.Encrypt([]byte(`{"access_token":"super-secret"}`))
	if err != nil {
		log.Fatal(err)
	}

	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("recovered: %s", plaintext)
}
```

## Notes

- `NewAESGCMEncryptor` requires a key of exactly `secure.KeySize` (32) bytes and returns `ErrInvalidKeySize` otherwise
- `Encrypt` prepends the nonce to the returned ciphertext; `Decrypt` expects that same layout and returns `ErrCiphertextTooShort` if the input is too short to contain a nonce
- `Decrypt` returns an error if authentication fails, which covers both a tampered ciphertext and the wrong key
- the encryption key is supplied by the caller and is never derived, stored, or logged by this package key generation, rotation, and storage are the caller's responsibility
- never construct ciphertext by hand or reuse a nonce with the same key: nonce reuse under AES-GCM breaks both confidentiality and authenticity of every message that shares it

## Reference

### Constants and errors

| Name                    | Description                                                                 |
| ----------------------- | --------------------------------------------------------------------------- |
| `KeySize`               | Required key length for `NewAESGCMEncryptor`: 32 bytes (AES-256)            |
| `ErrInvalidKeySize`     | Returned when the key passed to `NewAESGCMEncryptor` is not `KeySize` bytes |
| `ErrCiphertextTooShort` | Returned by `Decrypt` when the input is shorter than the nonce size         |