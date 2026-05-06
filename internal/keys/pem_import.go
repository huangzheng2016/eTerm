package keys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
)

// NormalizePEMInput trims outer whitespace, removes a UTF-8 BOM, strips common
// zero-width characters, and maps non-breaking spaces to ASCII space.
func NormalizePEMInput(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "\ufeff")
	repl := strings.NewReplacer(
		"\u200b", "", "\u200c", "", "\u200d", "", "\u2060", "", "\ufeff", "",
		"\u00a0", " ",
	)
	s = repl.Replace(s)
	return strings.TrimSpace(s)
}

func expandUserPath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~/"))
	}
	return p
}

// ImportKeyFromUserInput imports from pasted PEM text, or from a single-line path to a key file.
// Material is normalized with [NormalizePEMInput] before parsing.
func ImportKeyFromUserInput(database *gorm.DB, masterKey *security.MasterKeyManager, name, raw, certificatePath string, storageMode string) (*db.SSHKey, error) {
	clean := NormalizePEMInput(raw)
	if clean == "" {
		return nil, fmt.Errorf("empty key material")
	}

	if strings.Contains(clean, "BEGIN") {
		return importPrivateKeyRecord(database, masterKey, name, []byte(clean), storageMode, "", detectCertificatePath("", expandUserPath(certificatePath)))
	}

	// Single-line file path (no PEM header): read from disk; reference path only in "file" storage mode.
	if !strings.Contains(clean, "\n") && !strings.Contains(clean, "\r") {
		path := expandUserPath(clean)
		pemData, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read key file: %w", err)
		}
		refPath := ""
		if storageMode == "file" {
			refPath = path
		}
		return importPrivateKeyRecord(database, masterKey, name, pemData, storageMode, refPath, detectCertificatePath(path, expandUserPath(certificatePath)))
	}

	// Bare PEM without BEGIN line is rare; try parsing normalized bytes once.
	return importPrivateKeyRecord(database, masterKey, name, []byte(clean), storageMode, "", detectCertificatePath("", expandUserPath(certificatePath)))
}
