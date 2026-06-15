package sync

import (
	"crypto/sha256"
	"encoding/hex"
)

const tenantSalt = "eterm-tenant-v1:"

func TenantIDFromPassphrase(passphrase string) string {
	if passphrase == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(tenantSalt + passphrase))
	return hex.EncodeToString(sum[:])
}
