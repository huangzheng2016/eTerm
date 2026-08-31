package app

import (
	"time"

	"github.com/huangzheng2016/eTerm/internal/ai"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
)

// aiSessionHistoryCap bounds the persisted history JSON (~1MB).
const aiSessionHistoryCap = 1 << 20

type aiSession struct {
	ID        string `gorm:"primaryKey;size:32"`
	Title     string `gorm:"not null;default:''"`
	Provider  string `gorm:"not null;default:''"`
	Model     string `gorm:"not null;default:''"`
	CreatedAt time.Time
	UpdatedAt time.Time
	ForkOf    string `gorm:"not null;default:''"`
	History   []byte `gorm:"type:blob"`
}

func (aiSession) TableName() string { return "ai_sessions" }

// SaveSession implements aiview.SessionStore: export the agent history and
// upsert the session row. Empty history never creates a row, but an existing
// row is updated (e.g. emptied by /undo).
func (b *aiBridge) SaveSession(id, title, forkOf string) {
	b.mu.Lock()
	agent := b.agent
	history := b.pendingHistory
	b.mu.Unlock()
	if agent != nil {
		history, _ = agent.ExportHistory(aiSessionHistoryCap)
	}
	now := time.Now()
	updates := map[string]any{
		"history":    history,
		"provider":   b.store.ActiveProvider,
		"model":      b.store.ActiveModel,
		"updated_at": now,
	}
	if title != "" {
		updates["title"] = title
	}
	res := b.db.Model(&aiSession{}).Where("id = ?", id).Updates(updates)
	if res.Error == nil && res.RowsAffected == 0 && len(history) > 0 {
		_ = b.db.Create(&aiSession{
			ID: id, Title: title, Provider: b.store.ActiveProvider, Model: b.store.ActiveModel,
			ForkOf: forkOf, History: history, CreatedAt: now, UpdatedAt: now,
		}).Error
	}
}

// Sessions implements aiview.SessionStore: most recently updated first.
func (b *aiBridge) Sessions() []aiview.SessionEntry {
	var rows []aiSession
	if err := b.db.Select("id", "title", "provider", "model", "updated_at").
		Order("updated_at desc").Limit(50).Find(&rows).Error; err != nil {
		return nil
	}
	out := make([]aiview.SessionEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, aiview.SessionEntry{
			ID: r.ID, Title: r.Title, Provider: r.Provider, Model: r.Model, UpdatedAt: r.UpdatedAt,
		})
	}
	return out
}

// LoadSession implements aiview.SessionStore: import the row's history into
// the agent (or stash it until the first agent exists) and return it so the
// panel can rebuild its blocks.
func (b *aiBridge) LoadSession(id string) ([]byte, bool) {
	var row aiSession
	if err := b.db.Select("history").Where("id = ?", id).First(&row).Error; err != nil {
		return nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agent != nil {
		_ = b.agent.ImportHistory(row.History)
	} else {
		b.pendingHistory = row.History
	}
	return row.History, true
}

// UndoLastTurn implements aiview.SessionStore.
func (b *aiBridge) UndoLastTurn() {
	b.mu.Lock()
	agent := b.agent
	if agent == nil && b.pendingHistory != nil {
		if data, err := ai.UndoLastTurnJSON(b.pendingHistory); err == nil {
			b.pendingHistory = data
		}
	}
	b.mu.Unlock()
	if agent != nil {
		agent.UndoLastTurn()
	}
}

// ResetHistory implements aiview.SessionStore. Synchronous: slash commands
// are rejected while a run is active, so the agent mutex is free.
func (b *aiBridge) ResetHistory() {
	b.mu.Lock()
	agent := b.agent
	b.pendingHistory = nil
	b.mu.Unlock()
	if agent != nil {
		agent.Clear()
	}
}
