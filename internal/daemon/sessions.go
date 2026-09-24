package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/localterm"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/types"
)

const maxDaemonSessions = 32

type namedSession struct {
	name      string
	createdAt time.Time
	streamID  uint32
}

func newNamedSessionName() string {
	return "shell-" + uuid.NewString()[:6]
}

func (m *sessionManager) namedAdd(id, name string, streamID uint32, createdAt time.Time) {
	m.mu.Lock()
	m.named[id] = &namedSession{name: name, createdAt: createdAt, streamID: streamID}
	m.mu.Unlock()
}

func (m *sessionManager) namedGet(id string) *namedSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.named[id]
}

func (m *sessionManager) namedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.named)
}

func (m *sessionManager) addNamed(id, name string, streamID uint32, sr *streamRelay, createdAt time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.named) >= maxDaemonSessions {
		return false
	}
	m.streams[streamID] = sr
	m.named[id] = &namedSession{name: name, createdAt: createdAt, streamID: streamID}
	return true
}

func (m *sessionManager) rekeyNamed(id string, newStreamID uint32) (sr *streamRelay, existed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := m.named[id]
	if ns == nil {
		return nil, false
	}
	sr = m.streams[ns.streamID]
	if sr == nil {
		delete(m.named, id)
		return nil, true
	}
	delete(m.streams, ns.streamID)
	m.streams[newStreamID] = sr
	ns.streamID = newStreamID
	return sr, true
}

func (m *sessionManager) removeNamed(id string) *streamRelay {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := m.named[id]
	if ns == nil {
		return nil
	}
	delete(m.named, id)
	sr := m.streams[ns.streamID]
	delete(m.streams, ns.streamID)
	return sr
}

func (m *sessionManager) renameNamed(id, newName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := m.named[id]
	if ns == nil {
		return false
	}
	ns.name = newName
	return true
}

func (m *sessionManager) isPersistent(streamID uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ns := range m.named {
		if ns.streamID == streamID {
			return true
		}
	}
	return false
}

func (m *sessionManager) namedList() []types.TmuxSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]types.TmuxSession, 0, len(m.named))
	for id, ns := range m.named {
		sr := m.streams[ns.streamID]
		if sr == nil {
			continue
		}
		sr.mu.Lock()
		attached := sr.detachedSince.IsZero()
		sr.mu.Unlock()
		out = append(out, types.TmuxSession{Name: ns.name, SessionID: id, CreatedUnix: ns.createdAt.Unix(), Attached: attached, Daemon: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedUnix < out[j].CreatedUnix })
	return out
}

func daemonSessionList(mgr *sessionManager, sender *frameSender, streamID uint32) {
	payload, _ := json.Marshal(mgr.namedList())
	if sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID, Payload: payload}) == nil {
		_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: streamID})
	}
}

func daemonSessionNew(rt *runtimeConfig, mgr *sessionManager, sender *frameSender, streamID uint32, rows, cols int, streamCtx context.Context) {
	openErr := func(err error) {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: streamID, Payload: []byte(err.Error())})
	}
	if mgr.namedCount() >= maxDaemonSessions {
		openErr(fmt.Errorf("too many sessions (max %d)", maxDaemonSessions))
		return
	}
	configured, _ := db.GetSetting(rt.db, localterm.SettingShell)
	is, err := localNewSession(localterm.DefaultShell(configured), rows, cols)
	if err != nil {
		openErr(err)
		return
	}
	if err := waitSessionStarted(is); err != nil {
		_ = is.Close()
		openErr(err)
		return
	}
	name := newNamedSessionName()
	id := uuid.NewString()
	createdAt := time.Now()
	sr := newStreamRelay(is)
	if !mgr.addNamed(id, name, streamID, sr, createdAt) {
		sr.shutdown()
		_ = is.Close()
		openErr(fmt.Errorf("too many sessions (max %d)", maxDaemonSessions))
		return
	}
	payload, _ := json.Marshal(relay.TmuxSessionInfo{Name: name, SessionID: id, CreatedUnix: createdAt.Unix(), Attached: true, Daemon: true})
	if err := sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID, Payload: payload}); err != nil {
		if _, ok := mgr.removeStream(sr); ok {
			sr.shutdown()
			_ = is.Close()
		}
		return
	}
	go sr.pump(streamCtx, streamID, mgr)
	log.Printf("eterm daemon session new name=%q stream=%d", name, streamID)
}

func daemonSessionAttach(mgr *sessionManager, sender *frameSender, streamID uint32, id string, resumeFromSeq uint64) {
	openErr := func(err error) {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: streamID, Payload: []byte(err.Error())})
	}
	mgr.attachMu.Lock()
	defer mgr.attachMu.Unlock()
	ns := mgr.namedGet(id)
	if ns == nil {
		openErr(errors.New("no such session: " + id))
		return
	}
	oldStreamID := ns.streamID
	sr := mgr.get(oldStreamID)
	if sr == nil {
		openErr(errors.New("session is gone: " + ns.name))
		return
	}
	if !sr.canAttach(resumeFromSeq) {
		openErr(errors.New(resumeUnavailableErr))
		return
	}
	if oldStreamID != 0 && oldStreamID != streamID {
		_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: oldStreamID, Payload: []byte(relay.CloseSessionTakenOver)})
	}
	sr, existed := mgr.rekeyNamed(id, streamID)
	if !existed {
		openErr(errors.New("no such session: " + id))
		return
	}
	if sr == nil {
		openErr(errors.New("session is gone: " + ns.name))
		return
	}
	openOK := relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID}
	if err := sr.attachClamped(streamID, resumeFromSeq, sender, openOK); err != nil {
		openErr(errors.New(resumeUnavailableErr))
		return
	}
	log.Printf("eterm daemon session attach id=%q name=%q stream=%d old_stream=%d takeover=%t", id, ns.name, streamID, oldStreamID, oldStreamID != 0 && oldStreamID != streamID)
}

func daemonSessionKill(mgr *sessionManager, sender *frameSender, streamID uint32, id string) {
	mgr.attachMu.Lock()
	sr := mgr.removeNamed(id)
	mgr.attachMu.Unlock()
	log.Printf("eterm daemon session kill id=%q found=%t", id, sr != nil)
	if sr == nil {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: streamID, Payload: []byte("no such session: " + id)})
		return
	}
	sr.shutdown()
	_ = sr.is.Close()
	if sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID}) == nil {
		_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: streamID})
	}
}

func daemonSessionRename(mgr *sessionManager, sender *frameSender, streamID uint32, id, newName string) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: streamID, Payload: []byte("empty session name")})
		return
	}
	if !mgr.renameNamed(id, newName) {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: streamID, Payload: []byte("cannot rename session")})
		return
	}
	log.Printf("eterm daemon session rename id=%q new=%q", id, newName)
	if sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID}) == nil {
		_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: streamID})
	}
}
