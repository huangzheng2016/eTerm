package sshview

import (
	"errors"
	"log"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	watchdogStallAfter   = 15 * time.Second
	watchdogMaxHits      = 3
	watchdogTickInterval = time.Second
)

var errOutputStalled = errors.New("stream stalled: input without output")

type watchdogTickMsg struct {
	StreamID uint64
}

func (m *Model) noteWatchdogInput(now time.Time) {
	m.wdMu.Lock()
	m.wdInputAt = now
	m.wdBaseAt = now
	m.wdMu.Unlock()
}

func (m *Model) noteWatchdogOutput(now time.Time) {
	m.wdMu.Lock()
	m.wdOutputAt = now
	m.wdHits = 0
	m.wdMu.Unlock()
}

func (m *Model) resetWatchdog() {
	m.wdMu.Lock()
	m.wdInputAt = time.Time{}
	m.wdOutputAt = time.Time{}
	m.wdBaseAt = time.Time{}
	m.wdHits = 0
	m.wdMu.Unlock()
}

func (m *Model) watchdogTick() tea.Cmd {
	streamID := m.streamID
	every := m.wdTickEvery
	if every <= 0 {
		every = watchdogTickInterval
	}
	return tea.Tick(every, func(time.Time) tea.Msg {
		return watchdogTickMsg{StreamID: streamID}
	})
}

func (m *Model) ensureWatchdogTick() tea.Cmd {
	if m.wdStarted {
		return nil
	}
	m.wdStarted = true
	return m.watchdogTick()
}

func (m *Model) checkWatchdog(now time.Time) {
	if m.disconnected || m.reconnecting || m.wdStallAfter <= 0 || m.wdMaxHits <= 0 {
		return
	}
	m.mu.Lock()
	failed := m.endErr != nil
	m.mu.Unlock()
	if failed {
		return
	}
	m.wdMu.Lock()
	base := m.wdBaseAt
	if base.IsZero() || now.Sub(base) < m.wdStallAfter || m.wdOutputAt.After(base) {
		m.wdMu.Unlock()
		return
	}
	m.wdHits++
	hits := m.wdHits
	m.wdBaseAt = now
	m.wdMu.Unlock()
	log.Printf("eterm sshview: watchdog stall hit %d/%d stream=%d idle=%s", hits, m.wdMaxHits, m.streamID, now.Sub(base))
	if hits < m.wdMaxHits {
		return
	}
	m.resetWatchdog()
	log.Printf("eterm sshview: watchdog reconnecting stalled stream=%d alias=%q", m.streamID, m.alias)
	m.mu.Lock()
	if m.endErr == nil {
		m.endErr = errOutputStalled
	}
	m.mu.Unlock()
	if sess := m.currentSession(); sess != nil {
		_ = sess.Close()
	}
}
