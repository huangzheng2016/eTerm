package syncd

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
)

const ShareAbsMaxHours = 168
const ShareDefaultMaxHours = 4

var ErrShareNotFound = errors.New("share not found")

type ShareEntry struct {
	ID        string    `gorm:"primaryKey"`
	Tenant    string    `gorm:"index;not null;default:''"`
	Token     string    `gorm:"uniqueIndex;not null"`
	PeerID    string    `gorm:"not null"`
	Name      string
	Target    string `gorm:"not null;default:'local'"`
	SessionID string
	CreatedAt time.Time
	ExpiresAt time.Time `gorm:"index"`
	MaxHours  int       `gorm:"not null"`
}

func clampShareHours(h int) int {
	if h <= 0 {
		return ShareDefaultMaxHours
	}
	if h > ShareAbsMaxHours {
		return ShareAbsMaxHours
	}
	return h
}

func randomShareID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "shr_" + hex.EncodeToString(b[:]), nil
}

func (e *Engine) CreateShare(tenant, peerID, name, target, sessionID string, maxHours int) (*ShareEntry, error) {
	maxHours = clampShareHours(maxHours)
	if target == "" {
		target = relay.TargetLocal
	}
	id, err := randomShareID()
	if err != nil {
		return nil, err
	}
	token, err := randomDownloadToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	entry := &ShareEntry{
		ID:        id,
		Tenant:    tenant,
		Token:     token,
		PeerID:    peerID,
		Name:      name,
		Target:    target,
		SessionID: sessionID,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(maxHours) * time.Hour),
		MaxHours:  maxHours,
	}
	if err := e.DB.Create(entry).Error; err != nil {
		return nil, err
	}
	return entry, nil
}

func (e *Engine) GetShareByToken(token string) (*ShareEntry, error) {
	var entry ShareEntry
	if err := e.DB.Where("token = ?", token).First(&entry).Error; err != nil {
		return nil, ErrShareNotFound
	}
	return e.validShare(&entry)
}

func (e *Engine) validShare(entry *ShareEntry) (*ShareEntry, error) {
	if !entry.ExpiresAt.After(time.Now().UTC()) {
		_ = e.DB.Delete(entry).Error
		return nil, ErrShareNotFound
	}
	return entry, nil
}

func (e *Engine) RenewShare(tenant, token string) (*ShareEntry, error) {
	var entry ShareEntry
	if err := e.DB.Where("tenant = ? AND token = ?", tenant, token).First(&entry).Error; err != nil {
		return nil, ErrShareNotFound
	}
	if _, err := e.validShare(&entry); err != nil {
		return nil, err
	}
	entry.ExpiresAt = time.Now().UTC().Add(time.Duration(entry.MaxHours) * time.Hour)
	if err := e.DB.Save(&entry).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func (e *Engine) DeleteShare(tenant, token string) error {
	return e.DB.Where("tenant = ? AND token = ?", tenant, token).Delete(&ShareEntry{}).Error
}

func (e *Engine) CleanupExpiredShares() error {
	return e.DB.Where("expires_at <= ?", time.Now().UTC()).Delete(&ShareEntry{}).Error
}
