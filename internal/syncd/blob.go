package syncd

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

const MaxBlobBytes = 10 * 1024 * 1024
const BlobTTL = 30 * time.Minute

var ErrBlobTooLarge = errors.New("blob exceeds 10 MiB")
var ErrBlobNotFound = errors.New("blob not found")

type BlobEntry struct {
	ID            string `gorm:"primaryKey"`
	Tenant        string `gorm:"index;not null;default:''"`
	Mime          string `gorm:"not null"`
	Filename      string
	Data          []byte `gorm:"type:blob;not null"`
	Bytes         int64  `gorm:"not null"`
	DownloadToken string `gorm:"uniqueIndex;not null"`
	CreatedAt     time.Time
	ExpiresAt     time.Time `gorm:"index"`
}

func randomBlobID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "blb_" + hex.EncodeToString(b[:]), nil
}

func randomDownloadToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
