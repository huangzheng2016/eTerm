package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/ai"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/security"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"gorm.io/gorm"
)

const aiSendKeysTailBytes = 2048

// Bounds for the send_keys completion wait (OSC 133;D). Vars so tests can shrink them.
var (
	aiSendKeysMaxWait      = 10 * time.Second
	aiSendKeysPollInterval = 100 * time.Millisecond
)

// aiSharedState is read from the agent goroutine and written on the UI
// goroutine; the mutex guards the daemon peer list.
type aiSharedState struct {
	mu    sync.RWMutex
	peers []types.RemotePeer
}

func (s *aiSharedState) setPeers(peers []types.RemotePeer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers = peers
}

func (s *aiSharedState) peerByName(name string) (types.RemotePeer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.peers {
		if p.Name == name {
			return p, true
		}
	}
	return types.RemotePeer{}, false
}

func (s *aiSharedState) daemonInfos() []ai.DaemonInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ai.DaemonInfo, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, ai.DaemonInfo{Name: p.Name, Status: "online"})
	}
	return out
}

type aiToolOp int

const (
	aiToolListTabs aiToolOp = iota
	aiToolReadTab
	aiToolSendKeys
	aiToolEnterDaemon
	aiToolCreateSession
	aiToolRenameSession
)

// aiToolRequest is posted by the executor (agent goroutine) and answered on
// the UI goroutine: terminal/tab state is only safe to touch there.
type aiToolRequest struct {
	op       aiToolOp
	ctx      context.Context
	id       string // tab id or daemon name
	arg      string // keys / session name / old name
	arg2     string // new name (rename)
	maxBytes int
	skip     int
	waitMs   int
	resp     chan aiToolResult
}

type aiToolResult struct {
	text  string
	total int
	tabs  []ai.TabInfo
	err   error
}

func (req aiToolRequest) respond(r aiToolResult) {
	// Buffered per-call channel: never blocks, even after the caller gave up.
	req.resp <- r
}

type aiToolRequestMsg struct{ req aiToolRequest }

// aiToolSendKeysDoneMsg fires after the minimum wait and on each later poll:
// before is the OSC 133;D count at send time, deadline bounds the total wait.
type aiToolSendKeysDoneMsg struct {
	req      aiToolRequest
	before   int
	deadline time.Time
}
type aiToolRenameDoneMsg struct {
	req  aiToolRequest
	peer types.RemotePeer
	err  error
}

// waitAIToolRequest pumps one executor request into the Update loop; the
// handler re-arms it (pattern: sshview.waitChunk).
func waitAIToolRequest(ch <-chan aiToolRequest) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-ch
		if !ok {
			return nil
		}
		return aiToolRequestMsg{req: req}
	}
}

// aiExecutor implements ai.Executor. Tab ops round-trip to the UI goroutine;
// daemon list/session ops are plain network calls and run on the caller's
// goroutine.
type aiExecutor struct {
	db     *gorm.DB
	mk     *security.MasterKeyManager
	reqCh  chan<- aiToolRequest
	shared *aiSharedState
}

func (e *aiExecutor) roundTrip(ctx context.Context, req aiToolRequest) (aiToolResult, error) {
	req.ctx = ctx
	req.resp = make(chan aiToolResult, 1)
	select {
	case e.reqCh <- req:
	case <-ctx.Done():
		return aiToolResult{}, ctx.Err()
	}
	select {
	case r := <-req.resp:
		return r, r.err
	case <-ctx.Done():
		return aiToolResult{}, ctx.Err()
	}
}

func (e *aiExecutor) ListTabs(ctx context.Context) ([]ai.TabInfo, error) {
	r, err := e.roundTrip(ctx, aiToolRequest{op: aiToolListTabs})
	return r.tabs, err
}

func (e *aiExecutor) ReadTab(ctx context.Context, id string, maxBytes, skipFromEnd int) (string, int, error) {
	r, err := e.roundTrip(ctx, aiToolRequest{op: aiToolReadTab, id: id, maxBytes: maxBytes, skip: skipFromEnd})
	return r.text, r.total, err
}

func (e *aiExecutor) SendKeys(ctx context.Context, id string, keys string, waitMs int) (string, error) {
	r, err := e.roundTrip(ctx, aiToolRequest{op: aiToolSendKeys, id: id, arg: keys, waitMs: waitMs})
	return r.text, err
}

func (e *aiExecutor) EnterDaemon(ctx context.Context, daemon, session string) error {
	_, err := e.roundTrip(ctx, aiToolRequest{op: aiToolEnterDaemon, id: daemon, arg: session})
	return err
}

func (e *aiExecutor) CreateSession(ctx context.Context, daemon, name string) error {
	_, err := e.roundTrip(ctx, aiToolRequest{op: aiToolCreateSession, id: daemon, arg: name})
	return err
}

func (e *aiExecutor) RenameSession(ctx context.Context, daemon, oldName, newName string) error {
	_, err := e.roundTrip(ctx, aiToolRequest{op: aiToolRenameSession, id: daemon, arg: oldName, arg2: newName})
	return err
}

func (e *aiExecutor) ListDaemons(ctx context.Context) ([]ai.DaemonInfo, error) {
	return e.shared.daemonInfos(), nil
}

func (e *aiExecutor) remoteBase() (string, esync.Config, *esync.Tunnel, error) {
	cfg := esync.LoadConfig(e.db, e.mk)
	base, tunnel, err := syncHTTPBaseFor(e.db, e.mk, cfg)
	return base, cfg, tunnel, err
}

func (e *aiExecutor) ListDaemonSessions(ctx context.Context, daemon string) ([]ai.SessionInfo, error) {
	peer, ok := e.shared.peerByName(daemon)
	if !ok {
		return nil, fmt.Errorf("unknown daemon: %s", daemon)
	}
	base, cfg, tunnel, err := e.remoteBase()
	if err != nil {
		return nil, err
	}
	if tunnel != nil {
		defer tunnel.Close()
	}
	sessions, err := remoteListTmuxSessions(ctx, base, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID)
	if err != nil {
		return nil, err
	}
	out := make([]ai.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, ai.SessionInfo{Name: s.Name, Attached: s.Attached})
	}
	return out, nil
}

func (e *aiExecutor) KillSession(ctx context.Context, daemon, name string) error {
	peer, ok := e.shared.peerByName(daemon)
	if !ok {
		return fmt.Errorf("unknown daemon: %s", daemon)
	}
	base, cfg, tunnel, err := e.remoteBase()
	if err != nil {
		return err
	}
	if tunnel != nil {
		defer tunnel.Close()
	}
	return remoteKillTmuxSession(ctx, base, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, name)
}

// --- UI-goroutine side (called from App.Update) ---

func (a App) handleAIToolRequest(req aiToolRequest) (App, tea.Cmd) {
	switch req.op {
	case aiToolListTabs:
		req.respond(aiToolResult{tabs: a.aiTabInfos()})
	case aiToolReadTab:
		m := a.sshViewByAITabID(req.id)
		if m == nil {
			req.respond(aiToolResult{err: fmt.Errorf("unknown or non-terminal tab id: %s", req.id)})
			break
		}
		text, total := windowTranscript(m.PlainTranscript(sshview.MaxTranscriptBytes), req.maxBytes, req.skip)
		req.respond(aiToolResult{text: text, total: total})
	case aiToolSendKeys:
		m := a.sshViewByAITabID(req.id)
		if m == nil {
			req.respond(aiToolResult{err: fmt.Errorf("unknown or non-terminal tab id: %s", req.id)})
			break
		}
		if !m.SendRaw(decodeSendKeys(req.arg)) {
			req.respond(aiToolResult{err: fmt.Errorf("tab %s is not writable", req.id)})
			break
		}
		waitMs := req.waitMs
		if waitMs <= 0 {
			waitMs = 300
		}
		// Answer after the wait so the screen tail reflects the command
		// output; the UI loop keeps processing chunks meanwhile. The done
		// handler keeps polling for command completion (OSC 133;D).
		done := aiToolSendKeysDoneMsg{
			req:      req,
			before:   m.CommandCount(),
			deadline: time.Now().Add(aiSendKeysMaxWait),
		}
		return a, tea.Tick(time.Duration(waitMs)*time.Millisecond, func(time.Time) tea.Msg {
			return done
		})
	case aiToolEnterDaemon, aiToolCreateSession:
		peer, ok := a.aiShared.peerByName(req.id)
		if !ok {
			req.respond(aiToolResult{err: fmt.Errorf("unknown daemon: %s", req.id)})
			break
		}
		open := types.RemoteShellOpenMsg{Peer: peer}
		switch {
		case req.op == aiToolCreateSession:
			open.Tmux = true
			open.Target = relay.TargetTmuxNew
			open.SessionID = req.arg
		case req.arg == "":
			open.Target = relay.TargetLocal
		default:
			open.Tmux = true
			open.Target = relay.TargetTmuxAttach
			open.SessionID = req.arg
		}
		var cmd tea.Cmd
		a, cmd = a.openRemoteShell(open)
		req.respond(aiToolResult{})
		return a, cmd
	case aiToolRenameSession:
		peer, ok := a.aiShared.peerByName(req.id)
		if !ok {
			req.respond(aiToolResult{err: fmt.Errorf("unknown daemon: %s", req.id)})
			break
		}
		database, mk := a.db, a.masterKey
		return a, func() tea.Msg {
			cfg := esync.LoadConfig(database, mk)
			base, tunnel, err := syncHTTPBaseFor(database, mk, cfg)
			if err == nil {
				if tunnel != nil {
					defer tunnel.Close()
				}
				err = remoteRenameTmuxSession(req.ctx, base, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, req.arg, req.arg2)
			}
			return aiToolRenameDoneMsg{req: req, peer: peer, err: err}
		}
	}
	return a, nil
}

func (a App) aiTabInfos() []ai.TabInfo {
	out := make([]ai.TabInfo, 0, len(a.tabs))
	for i, tab := range a.tabs {
		info := ai.TabInfo{Title: tab.Title, Type: string(tab.Type), Active: i == a.activeTab}
		if m, ok := tab.Model.(*sshview.Model); ok {
			info.ID = strconv.FormatUint(m.StreamID(), 10)
		} else {
			// Placeholder id; read/send on non-terminal tabs reports an error.
			info.ID = fmt.Sprintf("tab-%d", i)
		}
		out = append(out, info)
	}
	return out
}

func (a App) sshViewByAITabID(id string) *sshview.Model {
	if id == "" || strings.HasPrefix(id, "tab-") {
		return nil
	}
	sid, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return nil
	}
	for i := range a.tabs {
		if m, ok := a.tabs[i].Model.(*sshview.Model); ok && m.StreamID() == sid {
			return m
		}
	}
	return nil
}

// handleAIToolSendKeysDone answers a send_keys request once the command
// finished (the OSC 133;D count passed the snapshot taken at send time), the
// max wait expired, or the request context was cancelled. The wait only
// extends past the minimum while a command is actually in flight (133;C seen,
// 133;D pending); a stale count from an earlier 133-capable shell or a shell
// that never emits OSC 133 answers right after the minimum wait.
func (a App) handleAIToolSendKeysDone(msg aiToolSendKeysDoneMsg) (App, tea.Cmd) {
	req := msg.req
	m := a.sshViewByAITabID(req.id)
	if m == nil {
		req.respond(aiToolResult{err: fmt.Errorf("tab %s is gone", req.id)})
		return a, nil
	}
	if req.ctx != nil && req.ctx.Err() != nil {
		req.respond(aiToolResult{err: req.ctx.Err()})
		return a, nil
	}
	if m.CommandCount() <= msg.before && m.CommandRunning() && time.Now().Before(msg.deadline) {
		return a, tea.Tick(aiSendKeysPollInterval, func(time.Time) tea.Msg { return msg })
	}
	req.respond(aiToolResult{text: transcriptTail(m.PlainTranscript(sshview.MaxTranscriptBytes), aiSendKeysTailBytes)})
	return a, nil
}

// decodeSendKeys decodes escape sequences in an AI-provided keys string
// before it is written to the pty: \\ -> \, \n -> LF, \r -> CR, \t -> TAB,
// \xHH -> raw byte. Unknown escapes, incomplete hex and raw control bytes
// pass through unchanged.
func decodeSendKeys(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case '\\':
			b.WriteByte('\\')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'x':
			hi, ok1 := hexVal(s, i+2)
			lo, ok2 := hexVal(s, i+3)
			if !ok1 || !ok2 {
				b.WriteByte('\\')
				continue
			}
			b.WriteByte(hi<<4 | lo)
			i += 2
		default:
			b.WriteByte('\\')
			continue
		}
		i++
	}
	return b.String()
}

func hexVal(s string, i int) (byte, bool) {
	if i >= len(s) {
		return 0, false
	}
	switch c := s[i]; {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// windowTranscript slices a tail-biased window out of the full transcript:
// up to maxBytes ending skipFromEnd bytes before the tail.
func windowTranscript(full string, maxBytes, skipFromEnd int) (string, int) {
	total := len(full)
	if maxBytes <= 0 {
		maxBytes = 8192
	}
	if skipFromEnd < 0 {
		skipFromEnd = 0
	}
	end := total - skipFromEnd
	if end < 0 {
		end = 0
	}
	start := end - maxBytes
	if start < 0 {
		start = 0
	}
	for start < end && !utf8.RuneStart(full[start]) {
		start++
	}
	for end < total && end > start && !utf8.RuneStart(full[end]) {
		end--
	}
	return full[start:end], total
}

// transcriptTail returns the last maxBytes of full, rune-aligned.
func transcriptTail(full string, maxBytes int) string {
	if len(full) <= maxBytes {
		return full
	}
	start := len(full) - maxBytes
	for start < len(full) && !utf8.RuneStart(full[start]) {
		start++
	}
	return full[start:]
}
