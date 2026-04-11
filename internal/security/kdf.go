package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/argon2"
)

func DeriveKey(password []byte, salt []byte) []byte {
	return argon2.IDKey(password, salt, 3, 64*1024, 4, 32)
}

func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	return salt, nil
}

func ComputeVerifier(key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("eterm-verify"))
	return mac.Sum(nil)
}
