package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/ai"
	"github.com/huangzheng2016/eTerm/internal/config"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/security"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/tmux"
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

// Bounds for the open_* tab wait: how often to re-list tabs and how long to
// wait for the new tab to appear (SSH dials can be slow). Vars so tests can
// shrink them.
var (
	aiOpenPollInterval = 250 * time.Millisecond
	aiOpenTabTimeout   = 15 * time.Second
	aiOpenSSHTimeout   = 60 * time.Second
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
	aiToolOpenLocal
	aiToolOpenSSH
	aiToolOpenTmux
	aiToolPollTab
	// aiToolCronFire is not a tool call: the cron scheduler posts a scheduled
	// wake through the same pump (app.go/app_update.go carry no cron case).
	aiToolCronFire
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
	// beforeIDs lists tab ids to exclude (aiToolPollTab: the snapshot taken
	// when the open was issued).
	beforeIDs []string
	resp      chan aiToolResult
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
	// openMu serializes open_* waits across the main agent and its
	// sub-agents (they all share this executor), so a tab landing mid-wait
	// is attributable to the in-flight request.
	openMu sync.Mutex
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

// openAndWaitTab issues an open request and polls until the new tab appears.
// The poll matches on creation-time identity (SSH host id, tmux session name,
// local-shell kind) rather than the tab title: a remote OSC 0/2 or a local
// PROMPT_COMMAND can retitle the tab right after creation, before the poll
// sees it. Opens are serialized (openMu), so a tab landing mid-wait belongs
// to the in-flight request; the before-snapshot captured atomically by the
// UI handler excludes pre-existing tabs of the same host/session. A failed
// open (unknown session, dial error) never lands a tab, so it surfaces as a
// wait timeout. One accepted gap: a local shell tab opened by the user in the
// same sub-second window is indistinguishable from ours (local tabs carry no
// host/session identity).
func (e *aiExecutor) openAndWaitTab(ctx context.Context, req aiToolRequest, timeout time.Duration) (string, error) {
	e.openMu.Lock()
	defer e.openMu.Unlock()
	r, err := e.roundTrip(ctx, req)
	if err != nil {
		return "", err
	}
	kind := ""
	switch req.op {
	case aiToolOpenSSH:
		kind = "ssh"
	case aiToolOpenTmux:
		kind = "tmux"
	case aiToolOpenLocal:
		kind = "local"
	}
	before := make([]string, 0, len(r.tabs))
	for _, t := range r.tabs {
		before = append(before, t.ID)
	}
	poll := aiToolRequest{op: aiToolPollTab, id: kind, arg: r.text, beforeIDs: before}
	deadline := time.Now().Add(timeout)
	for {
		pr, err := e.roundTrip(ctx, poll)
		if err != nil {
			return "", err
		}
		if pr.text != "" {
			return pr.text, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for the new tab; the open may have failed")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(aiOpenPollInterval):
		}
	}
}

func (e *aiExecutor) OpenLocalTerminal(ctx context.Context) (string, error) {
	return e.openAndWaitTab(ctx, aiToolRequest{op: aiToolOpenLocal}, aiOpenTabTimeout)
}

func (e *aiExecutor) OpenSSH(ctx context.Context, host string) (string, error) {
	return e.openAndWaitTab(ctx, aiToolRequest{op: aiToolOpenSSH, id: host}, aiOpenSSHTimeout)
}

func (e *aiExecutor) OpenTmux(ctx context.Context, session string) (string, error) {
	sessions, err := e.ListTmuxSessions(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range sessions {
		if s.Name == session {
			return e.openAndWaitTab(ctx, aiToolRequest{op: aiToolOpenTmux, arg: session}, aiOpenTabTimeout)
		}
	}
	return "", fmt.Errorf("unknown tmux session: %s", session)
}

func (e *aiExecutor) ListHosts(ctx context.Context) ([]ai.HostInfo, error) {
	var hosts []db.Host
	if err := e.db.WithContext(ctx).Order("alias, hostname").Find(&hosts).Error; err != nil {
		return nil, err
	}
	out := make([]ai.HostInfo, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, ai.HostInfo{
			Name:    hostDisplayName(h),
			Address: fmt.Sprintf("%s:%d", h.Hostname, h.Port),
			Tags:    h.Tags,
			ID:      h.ID,
		})
	}
	return out, nil
}

func (e *aiExecutor) ListTmuxSessions(ctx context.Context) ([]ai.SessionInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	configFile, err := tmux.ResolveConfig(e.db, config.ConfigDir(), home)
	if err != nil {
		return nil, err
	}
	sessions, err := tmux.ListSessions(ctx, configFile)
	if err != nil {
		return nil, err
	}
	out := make([]ai.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, ai.SessionInfo{Name: s.Name, Attached: s.Attached})
	}
	return out, nil
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
	case aiToolOpenLocal:
		// Respond before the tab exists: the answer carries the tab snapshot
		// and the poll matcher; the executor polls for the fresh tab id.
		before := a.aiTabInfos()
		var cmd tea.Cmd
		a, cmd = a.openLocalTerminal()
		req.respond(aiToolResult{tabs: before})
		return a, cmd
	case aiToolOpenTmux:
		before := a.aiTabInfos()
		var cmd tea.Cmd
		a, cmd = a.openTmux(types.TmuxOpenMsg{Name: req.arg})
		req.respond(aiToolResult{text: req.arg, tabs: before})
		return a, cmd
	case aiToolOpenSSH:
		matches := findHostsByName(a.db, req.id)
		if len(matches) == 0 {
			req.respond(aiToolResult{err: fmt.Errorf("unknown host: %s (see list_hosts)", req.id)})
			break
		}
		if len(matches) > 1 {
			var cands []string
			for _, h := range matches {
				cands = append(cands, fmt.Sprintf("%s:%d (id %d)", h.Hostname, h.Port, h.ID))
			}
			req.respond(aiToolResult{err: fmt.Errorf("host %q is ambiguous (%s); give the hosts unique aliases to open them by name", req.id, strings.Join(cands, ", "))})
			break
		}
		host := matches[0]
		before := a.aiTabInfos()
		req.respond(aiToolResult{text: strconv.FormatUint(uint64(host.ID), 10), tabs: before})
		return a, func() tea.Msg { return types.SSHConnectMsg{HostID: host.ID} }
	case aiToolPollTab:
		req.respond(aiToolResult{text: a.findFreshAITab(req.id, req.arg, req.beforeIDs)})
	case aiToolCronFire:
		// One-way (no respond): route the wake through the panel's normal
		// send path - a running turn queues it (dim Queued marker, acked by
		// EventSteer), an idle panel starts a new run with it.
		if a.aiView != nil {
			a.aiView.InsertText(req.arg)
			return a, a.aiView.SubmitInput()
		}
	}
	return a, nil
}

// findHostsByName resolves a list_hosts name (alias, user@host, or hostname -
// the same display name the command palette shows) to all matching DB rows.
func findHostsByName(database *gorm.DB, name string) []db.Host {
	var hosts []db.Host
	if err := database.Order("alias, hostname").Find(&hosts).Error; err != nil {
		return nil
	}
	var out []db.Host
	for _, h := range hosts {
		if hostDisplayName(h) == name {
			out = append(out, h)
		}
	}
	return out
}

// findFreshAITab returns the stream-id string of a tab created after the
// before snapshot, matched by creation-time identity (immune to OSC
// retitling): ssh = sshview host id, tmux = Tab.TmuxSession, local = a local
// tab without a tmux session.
func (a App) findFreshAITab(kind, arg string, before []string) string {
	skip := make(map[string]bool, len(before))
	for _, id := range before {
		skip[id] = true
	}
	for i := range a.tabs {
		m, ok := a.tabs[i].Model.(*sshview.Model)
		if !ok {
			continue
		}
		id := strconv.FormatUint(m.StreamID(), 10)
		if skip[id] {
			continue
		}
		switch kind {
		case "ssh":
			hostID, err := strconv.ParseUint(arg, 10, 64)
			if err != nil || m.HostID() != uint(hostID) {
				continue
			}
		case "tmux":
			if a.tabs[i].TmuxSession != arg {
				continue
			}
		case "local":
			if a.tabs[i].Type != LocalTab || a.tabs[i].TmuxSession != "" {
				continue
			}
		}
		return id
	}
	return ""
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
