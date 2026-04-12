package db

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (h *Host) BeforeCreate(tx *gorm.DB) error {
	if h.SyncID == "" {
		h.SyncID = uuid.New().String()
	}
	return nil
}

func (k *SSHKey) BeforeCreate(tx *gorm.DB) error {
	if k.SyncID == "" {
		k.SyncID = uuid.New().String()
	}
	return nil
}

func (s *Snippet) BeforeCreate(tx *gorm.DB) error {
	if s.SyncID == "" {
		s.SyncID = uuid.New().String()
	}
	return nil
}

func (p *PortForward) BeforeCreate(tx *gorm.DB) error {
	if p.SyncID == "" {
		p.SyncID = uuid.New().String()
	}
	return nil
}
