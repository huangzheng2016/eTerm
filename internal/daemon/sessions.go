package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/localterm"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/types"
)

// Named daemon-hosted shell sessions: a tmux substitute for platforms where
// no tmux binary is available (e.g. Windows). Sessions are plain local shells
// owned by the daemon; they live until killed or until the daemon exits.

const maxDaemonSessions = 32

type namedSession struct {
	name      string
	createdAt time.Time
	streamID  uint32
}

func newNamedSessionName() string {
	return "shell-" + uuid.NewString()[:6]
}

func (m *sessionManager) namedAdd(name string, streamID uint32, createdAt time.Time) {
	m.mu.Lock()
	m.named[name] = &namedSession{name: name, createdAt: createdAt, streamID: streamID}
	m.mu.Unlock()
}

func (m *sessionManager) namedGet(name string) *namedSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.named[name]
}

func (m *sessionManager) namedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.named)
}

// addNamed registers a new named session atomically, enforcing
// maxDaemonSessions; false means the limit is reached.
func (m *sessionManager) addNamed(name string, streamID uint32, sr *streamRelay, createdAt time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.named) >= maxDaemonSessions {
		return false
	}
	m.streams[streamID] = sr
	m.named[name] = &namedSession{name: name, createdAt: createdAt, streamID: streamID}
	return true
}

// rekeyNamed atomically moves a named session's stream onto newStreamID.
// existed is false when the name is unknown; a nil stream with existed true
// means the session vanished and its stale entry was dropped.
func (m *sessionManager) rekeyNamed(name string, newStreamID uint32) (sr *streamRelay, existed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := m.named[name]
	if ns == nil {
		return nil, false
	}
	sr = m.streams[ns.streamID]
	if sr == nil {
		delete(m.named, name)
		return nil, true
	}
	delete(m.streams, ns.streamID)
	m.streams[newStreamID] = sr
	ns.streamID = newStreamID
	return sr, true
}

// removeNamed deletes a named entry and its stream, returning the stream.
func (m *sessionManager) removeNamed(name string) *streamRelay {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := m.named[name]
	if ns == nil {
		return nil
	}
	delete(m.named, name)
	sr := m.streams[ns.streamID]
	delete(m.streams, ns.streamID)
	return sr
}

// renameNamed retitles a session; false when oldName is unknown or newName
// is already taken.
func (m *sessionManager) renameNamed(oldName, newName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := m.named[oldName]
	if ns == nil {
		return false
	}
	if _, taken := m.named[newName]; taken {
		return false
	}
	delete(m.named, oldName)
	ns.name = newName
	m.named[newName] = ns
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

// namedList reports daemon-hosted sessions in the tmux list shape.
func (m *sessionManager) namedList() []types.TmuxSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]types.TmuxSession, 0, len(m.named))
	for _, ns := range m.named {
		sr := m.streams[ns.streamID]
		if sr == nil {
			continue
		}
		sr.mu.Lock()
		attached := sr.detachedSince.IsZero()
		sr.mu.Unlock()
		out = append(out, types.TmuxSession{Name: ns.name, CreatedUnix: ns.createdAt.Unix(), Attached: attached, Daemon: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedUnix < out[j].CreatedUnix })
	return out
}

// daemonSessionList handles TargetTmuxList without tmux.
func daemonSessionList(mgr *sessionManager, sender *frameSender, streamID uint32) {
	payload, _ := json.Marshal(mgr.namedList())
	if sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID, Payload: payload}) == nil {
		_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: streamID})
	}
}

// daemonSessionNew handles TargetTmuxNew without tmux: start a persistent
// local shell and answer with its generated name.
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
	for mgr.namedGet(name) != nil {
		name = newNamedSessionName()
	}
	sr := newStreamRelay(is)
	if !mgr.addNamed(name, streamID, sr, time.Now()) {
		sr.shutdown()
		_ = is.Close()
		openErr(fmt.Errorf("too many sessions (max %d)", maxDaemonSessions))
		return
	}
	if err := sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID, Payload: []byte(name)}); err != nil {
		if _, ok := mgr.removeStream(sr); ok {
			sr.shutdown()
			_ = is.Close()
		}
		return
	}
	go sr.pump(streamCtx, streamID, mgr)
}

// daemonSessionAttach handles TargetTmuxAttach without tmux: move the named
// session's stream onto this connection and replay retained output. attachMu
// makes the rekey and the rewind atomic against a second attach or a kill,
// so the stream can never be split across two ids; a later attach wins.
func daemonSessionAttach(mgr *sessionManager, sender *frameSender, streamID uint32, name string, resumeFromSeq uint64) {
	openErr := func(err error) {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: streamID, Payload: []byte(err.Error())})
	}
	mgr.attachMu.Lock()
	defer mgr.attachMu.Unlock()
	// A previous client may still hold the old stream id; close it so that
	// client drops cleanly instead of hanging on an id that goes silent.
	// A failed send must not block the attach.
	var oldStreamID uint32
	if ns := mgr.namedGet(name); ns != nil {
		oldStreamID = ns.streamID
	}
	if oldStreamID != 0 && oldStreamID != streamID {
		_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: oldStreamID, Payload: []byte(relay.CloseSessionTakenOver)})
	}
	sr, existed := mgr.rekeyNamed(name, streamID)
	if !existed {
		openErr(errors.New("no such session: " + name))
		return
	}
	if sr == nil {
		openErr(errors.New("session is gone: " + name))
		return
	}
	openOK := relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID}
	if err := sr.attachClamped(streamID, resumeFromSeq, sender, openOK); err != nil {
		openErr(errors.New(resumeUnavailableErr))
	}
}

// daemonSessionKill handles TargetTmuxKill without tmux. attachMu keeps a
// kill from landing between another attach's rekey and rewind.
func daemonSessionKill(mgr *sessionManager, sender *frameSender, streamID uint32, name string) {
	mgr.attachMu.Lock()
	sr := mgr.removeNamed(name)
	mgr.attachMu.Unlock()
	if sr != nil {
		sr.shutdown()
		_ = sr.is.Close()
	}
	if sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID}) == nil {
		_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: streamID})
	}
}

// daemonSessionRename handles TargetTmuxRename without tmux.
func daemonSessionRename(mgr *sessionManager, sender *frameSender, streamID uint32, oldName, newName string) {
	if !mgr.renameNamed(oldName, newName) {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: streamID, Payload: []byte("cannot rename session")})
		return
	}
	if sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID}) == nil {
		_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: streamID})
	}
}
