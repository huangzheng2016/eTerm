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

var (
	aiSendKeysMaxWait      = 10 * time.Second
	aiSendKeysPollInterval = 100 * time.Millisecond
)

var (
	aiOpenPollInterval = 250 * time.Millisecond
	aiOpenTabTimeout   = 15 * time.Second
	aiOpenSSHTimeout   = 60 * time.Second
)

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
	aiToolNotify
	aiToolCronFire
)

type aiToolRequest struct {
	op        aiToolOp
	ctx       context.Context
	id        string
	arg       string
	arg2      string
	maxBytes  int
	skip      int
	waitMs    int
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
	req.resp <- r
}

type aiToolRequestMsg struct{ req aiToolRequest }

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

func waitAIToolRequest(ch <-chan aiToolRequest) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-ch
		if !ok {
			return nil
		}
		return aiToolRequestMsg{req: req}
	}
}

type aiExecutor struct {
	db     *gorm.DB
	mk     *security.MasterKeyManager
	reqCh  chan<- aiToolRequest
	shared *aiSharedState
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

func (e *aiExecutor) hasDaemons() bool {
	e.shared.mu.RLock()
	defer e.shared.mu.RUnlock()
	return len(e.shared.peers) > 0
}

func (e *aiExecutor) Notify(ctx context.Context, text string) error {
	_, err := e.roundTrip(ctx, aiToolRequest{op: aiToolNotify, arg: text})
	return err
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
	case aiToolNotify:
		req.respond(aiToolResult{})
		return a, tea.Raw(sshview.OSC9Sequence(req.arg))
	case aiToolCronFire:
		if a.aiView != nil {
			return a, a.aiView.InjectUserMessage(req.arg)
		}
	}
	return a, nil
}

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
