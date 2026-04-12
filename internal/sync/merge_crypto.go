package sync

import (
	"encoding/base64"
	"fmt"

	"github.com/eterm/eterm/internal/security"
)

func decryptPayload(payload, passphrase string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}
	return AgeDecrypt(raw, passphrase)
}

func encryptField(plaintext string, mk *security.MasterKeyManager) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	k := mk.GetKey()
	if k == nil {
		return "", fmt.Errorf("master key locked")
	}
	defer k.Clear()
	enc, err := security.Encrypt([]byte(plaintext), k.Bytes())
	if err != nil {
		return "", err
	}
	return enc, nil
}

// mustEncrypt calls encryptField and accumulates errors. Returns "" on failure.
type encAccum struct {
	mk  *security.MasterKeyManager
	err error
}

func (a *encAccum) enc(plaintext string) string {
	if a.err != nil {
		return ""
	}
	v, err := encryptField(plaintext, a.mk)
	if err != nil {
		a.err = err
		return ""
	}
	return v
}
