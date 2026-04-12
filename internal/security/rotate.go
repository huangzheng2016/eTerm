package security

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/eterm/eterm/internal/db"
	"gorm.io/gorm"
)

// RotateMasterPassword re-encrypts all ciphertext in the DB and replaces the master key verifier.
// When noPassword is true (no-password mode), currentPassword is ignored.
func RotateMasterPassword(gdb *gorm.DB, mkm *MasterKeyManager, currentPassword, newPassword []byte, noPassword bool) error {
	if len(newPassword) == 0 {
		return errors.New("new password cannot be empty")
	}

	oldKey := mkm.GetKey()
	if oldKey == nil {
		return errors.New("master key is locked")
	}
	defer oldKey.Clear()
	oldK := oldKey.Bytes()

	if !noPassword {
		if !mkm.VerifyPassword(currentPassword) {
			return errors.New("current password is incorrect")
		}
	}

	newSalt, err := GenerateSalt()
	if err != nil {
		return err
	}
	newDerived := DeriveKey(newPassword, newSalt)
	newVerifier := ComputeVerifier(newDerived)

	reenc := func(cipherB64 string) (string, error) {
		if cipherB64 == "" {
			return "", nil
		}
		plain, err := Decrypt(cipherB64, oldK)
		if err != nil {
			return "", err
		}
		defer ClearBytes(plain)
		return Encrypt(plain, newDerived)
	}

	err = gdb.Transaction(func(tx *gorm.DB) error {
		var hosts []db.Host
		if err := tx.Find(&hosts).Error; err != nil {
			return err
		}
		for i := range hosts {
			h := &hosts[i]
			np, err := reenc(h.Password)
			if err != nil {
				return fmt.Errorf("host %q password: %w", h.Alias, err)
			}
			npp, err := reenc(h.Passphrase)
			if err != nil {
				return fmt.Errorf("host %q passphrase: %w", h.Alias, err)
			}
			npx, err := reenc(h.ProxyPassword)
			if err != nil {
				return fmt.Errorf("host %q proxy password: %w", h.Alias, err)
			}
			if err := tx.Model(h).Updates(map[string]interface{}{
				"password":       np,
				"passphrase":     npp,
				"proxy_password": npx,
			}).Error; err != nil {
				return err
			}
		}

		var keys []db.SSHKey
		if err := tx.Find(&keys).Error; err != nil {
			return err
		}
		for i := range keys {
			k := &keys[i]
			npk, err := reenc(k.PrivateKeyData)
			if err != nil {
				return fmt.Errorf("key %q private data: %w", k.Name, err)
			}
			npp, err := reenc(k.Passphrase)
			if err != nil {
				return fmt.Errorf("key %q passphrase: %w", k.Name, err)
			}
			if err := tx.Model(k).Updates(map[string]interface{}{
				"private_key_data": npk,
				"passphrase":       npp,
			}).Error; err != nil {
				return err
			}
		}

		for _, pair := range []struct {
			key string
			err string
		}{
			{"sync_api_key", "sync API key"},
			{"sync_passphrase", "sync passphrase"},
		} {
			raw, gerr := db.GetSetting(tx, pair.key)
			if gerr != nil {
				if errors.Is(gerr, gorm.ErrRecordNotFound) {
					continue
				}
				return gerr
			}
			if raw == "" {
				continue
			}
			out, err := reenc(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", pair.err, err)
			}
			if err := db.SetSetting(tx, pair.key, out); err != nil {
				return err
			}
		}

		saltB64 := base64.StdEncoding.EncodeToString(newSalt)
		verB64 := base64.StdEncoding.EncodeToString(newVerifier)
		if err := db.SetSetting(tx, "encryption_salt", saltB64); err != nil {
			return err
		}
		if err := db.SetSetting(tx, "encryption_verifier", verB64); err != nil {
			return err
		}
		if err := db.SetSetting(tx, "no_password", "false"); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		ClearBytes(newDerived)
		return err
	}
	newSB := New(newDerived)
	ClearBytes(newDerived)
	mkm.ReplaceAfterRotation(newSalt, newVerifier, newSB)
	return nil
}
