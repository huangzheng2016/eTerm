package keys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
)

func ListKeys(database *gorm.DB) ([]db.SSHKey, error) {
	var keys []db.SSHKey
	err := database.Order("updated_at DESC, name ASC").Find(&keys).Error
	return keys, err
}

func GetKey(database *gorm.DB, id uint) (*db.SSHKey, error) {
	var key db.SSHKey
	err := database.First(&key, id).Error
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func CreateKey(database *gorm.DB, masterKey *security.MasterKeyManager, name, keyType string, bits int, passphrase string, storageMode string) (*db.SSHKey, error) {
	var privateKeyPEM []byte
	var publicKey, fingerprint string
	var err error

	switch strings.ToLower(keyType) {
	case "ed25519":
		privateKeyPEM, publicKey, fingerprint, err = GenerateED25519()
	case "rsa":
		privateKeyPEM, publicKey, fingerprint, err = GenerateRSA(bits)
	default:
		return nil, fmt.Errorf("unsupported key type: %s", keyType)
	}
	if err != nil {
		return nil, err
	}

	sshKey := db.SSHKey{
		Name:        name,
		Type:        strings.ToLower(keyType),
		PublicKeyData: publicKey,
		Fingerprint: fingerprint,
		Bits:        bits,
		StorageMode: storageMode,
	}

	switch storageMode {
	case "database":
		k := masterKey.GetKey()
		if k == nil {
			return nil, fmt.Errorf("master key is locked")
		}
		encrypted, err := security.Encrypt(privateKeyPEM, k.Bytes())
		k.Clear()
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt private key: %w", err)
		}
		sshKey.PrivateKeyData = encrypted
	case "file":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home dir: %w", err)
		}
		sshDir := filepath.Join(home, ".ssh")
		if err := os.MkdirAll(sshDir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create .ssh dir: %w", err)
		}
		privPath := filepath.Join(sshDir, name)
		if err := os.WriteFile(privPath, privateKeyPEM, 0600); err != nil {
			return nil, fmt.Errorf("failed to write private key: %w", err)
		}
		pubPath := privPath + ".pub"
		if err := os.WriteFile(pubPath, []byte(publicKey), 0644); err != nil {
			return nil, fmt.Errorf("failed to write public key: %w", err)
		}
		sshKey.PrivatePath = privPath
		sshKey.PublicPath = pubPath
	default:
		return nil, fmt.Errorf("unsupported storage mode: %s", storageMode)
	}

	if passphrase != "" {
		k := masterKey.GetKey()
		if k == nil {
			return nil, fmt.Errorf("master key is locked")
		}
		encrypted, err := security.Encrypt([]byte(passphrase), k.Bytes())
		k.Clear()
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt passphrase: %w", err)
		}
		sshKey.Passphrase = encrypted
	}

	if err := database.Create(&sshKey).Error; err != nil {
		return nil, err
	}

	return &sshKey, nil
}

func ImportKey(database *gorm.DB, masterKey *security.MasterKeyManager, name, privatePath string, storageMode string) (*db.SSHKey, error) {
	pemData, err := os.ReadFile(privatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}
	return importPrivateKeyRecord(database, masterKey, name, pemData, storageMode, privatePath, detectCertificatePath(privatePath, ""))
}

// importPrivateKeyRecord persists an imported key. When storageMode is "file" and
// sourcePathWhenFile is non-empty, that path is stored as the key location (existing file).
// When sourcePathWhenFile is empty with "file" mode, PEM is written under ~/.ssh (same as generate).
func importPrivateKeyRecord(database *gorm.DB, masterKey *security.MasterKeyManager, name string, pemData []byte, storageMode, sourcePathWhenFile, certificatePath string) (*db.SSHKey, error) {
	signer, err := ssh.ParsePrivateKey(pemData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	pubKey := signer.PublicKey()
	publicKeyStr := string(ssh.MarshalAuthorizedKey(pubKey))
	fingerprint := ssh.FingerprintSHA256(pubKey)
	keyType := pubKey.Type()

	sshKey := db.SSHKey{
		Name:          name,
		Type:          keyType,
		PublicKeyData: publicKeyStr,
		Fingerprint:   fingerprint,
		StorageMode:   storageMode,
		CertificatePath: certificatePath,
	}

	switch storageMode {
	case "database":
		k := masterKey.GetKey()
		if k == nil {
			return nil, fmt.Errorf("master key is locked")
		}
		encrypted, err := security.Encrypt(pemData, k.Bytes())
		k.Clear()
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt private key: %w", err)
		}
		sshKey.PrivateKeyData = encrypted
	case "file":
		if sourcePathWhenFile != "" {
			sshKey.PrivatePath = sourcePathWhenFile
			break
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home dir: %w", err)
		}
		sshDir := filepath.Join(home, ".ssh")
		if err := os.MkdirAll(sshDir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create .ssh dir: %w", err)
		}
		privPath := filepath.Join(sshDir, name)
		if err := os.WriteFile(privPath, pemData, 0600); err != nil {
			return nil, fmt.Errorf("failed to write private key: %w", err)
		}
		pubPath := privPath + ".pub"
		if err := os.WriteFile(pubPath, []byte(publicKeyStr), 0644); err != nil {
			return nil, fmt.Errorf("failed to write public key: %w", err)
		}
		sshKey.PrivatePath = privPath
		sshKey.PublicPath = pubPath
	default:
		return nil, fmt.Errorf("unsupported storage mode: %s", storageMode)
	}

	if err := database.Create(&sshKey).Error; err != nil {
		return nil, err
	}

	return &sshKey, nil
}

func detectCertificatePath(privatePath, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	if strings.TrimSpace(privatePath) == "" {
		return ""
	}
	candidate := privatePath + "-cert.pub"
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func ExportKey(database *gorm.DB, masterKey *security.MasterKeyManager, keyID uint, outputPath string) error {
	key, err := GetKey(database, keyID)
	if err != nil {
		return fmt.Errorf("failed to load key: %w", err)
	}

	if key.PrivateKeyData == "" {
		return fmt.Errorf("no private key data stored in database")
	}

	k := masterKey.GetKey()
	if k == nil {
		return fmt.Errorf("master key is locked")
	}
	decrypted, err := security.Decrypt(key.PrivateKeyData, k.Bytes())
	k.Clear()
	if err != nil {
		return fmt.Errorf("failed to decrypt private key: %w", err)
	}

	if err := os.WriteFile(outputPath, decrypted, 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	return nil
}

func DeleteKey(database *gorm.DB, id uint) error {
	return database.Delete(&db.SSHKey{}, id).Error
}

func GetPublicKey(database *gorm.DB, id uint) (string, error) {
	key, err := GetKey(database, id)
	if err != nil {
		return "", err
	}
	return key.PublicKeyData, nil
}
