package ai

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type TabInfo struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Active bool   `json:"active"`
}

type DaemonInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	OS     string `json:"os,omitempty"`
	Addr   string `json:"addr,omitempty"`
}

type SessionInfo struct {
	Name     string `json:"name"`
	Attached bool   `json:"attached,omitempty"`
}

type HostInfo struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Tags    string `json:"tags,omitempty"`
}

// Executor is implemented by the app layer. It performs the actual terminal
// and daemon operations behind the agent's tools.
type Executor interface {
	ListTabs(ctx context.Context) ([]TabInfo, error)
	// ReadTab returns a window of the tab's full transcript (scrollback +
	// visible screen): up to maxBytes ending skipFromEnd bytes before the
	// transcript tail, plus the total transcript size in bytes.
	ReadTab(ctx context.Context, id string, maxBytes, skipFromEnd int) (text string, totalBytes int, err error)
	// SendKeys decodes escape sequences in keys (\\ -> \, \n -> LF, \r -> CR,
	// \t -> TAB, \xHH -> raw byte; unknown escapes and raw control bytes pass
	// through unchanged), writes the result to the tab's pty stdin, waits
	// waitMs, and returns the tab's visible-screen tail.
	SendKeys(ctx context.Context, id string, keys string, waitMs int) (string, error)
	ListDaemons(ctx context.Context) ([]DaemonInfo, error)
	ListDaemonSessions(ctx context.Context, daemon string) ([]SessionInfo, error)
	EnterDaemon(ctx context.Context, daemon, session string) error
	CreateSession(ctx context.Context, daemon, name string) error
	RenameSession(ctx context.Context, daemon, oldName, newName string) error
	KillSession(ctx context.Context, daemon, name string) error
	// The open_* methods open a new terminal tab and return its tab id (as
	// reported by ListTabs) once it exists.
	OpenLocalTerminal(ctx context.Context) (tabID string, err error)
	ListHosts(ctx context.Context) ([]HostInfo, error)
	OpenSSH(ctx context.Context, host string) (tabID string, err error)
	ListTmuxSessions(ctx context.Context) ([]SessionInfo, error)
	OpenTmux(ctx context.Context, session string) (tabID string, err error)
}

type ListTabsInput struct{}

type ListTabsOutput struct {
	Tabs  []TabInfo `json:"tabs"`
	Error string    `json:"error,omitempty"`
}

type ReadTabInput struct {
	ID          string `json:"id" jsonschema_description:"Tab id from list_tabs"`
	MaxBytes    int    `json:"max_bytes,omitempty" jsonschema_description:"Max bytes of transcript to return (default 8192)"`
	SkipFromEnd int    `json:"skip_from_end,omitempty" jsonschema_description:"Bytes to skip back from the end of the transcript (default 0 = most recent content). Increase to page into earlier history"`
}

type ReadTabOutput struct {
	Content    string `json:"content"`
	TotalBytes int    `json:"total_bytes"`
	Error      string `json:"error,omitempty"`
}

type SendKeysInput struct {
	ID     string `json:"id" jsonschema_description:"Tab id from list_tabs"`
	Keys   string `json:"keys" jsonschema_description:"Keys typed into the pty. The executor decodes escape sequences before writing: \\\\ -> \\, \\n -> LF (Enter), \\r -> CR, \\t -> TAB, \\xHH -> raw byte (\\x03 = Ctrl+C, \\x04 = Ctrl+D, \\x1b = Esc, \\x7f = Backspace, \\x1b[A/B/C/D = arrows). Unknown escapes pass through unchanged; raw control bytes in the string also pass through. To type a literal backslash use \\\\. Examples: run a command = \"ls -la\\n\"; interrupt = \"\\x03\"; exit REPL = \"\\x04\""`
	WaitMs int    `json:"wait_ms,omitempty" jsonschema_description:"Milliseconds to wait for output before returning the screen tail (default 300)"`
}

type SendKeysOutput struct {
	Success bool   `json:"success"`
	Screen  string `json:"screen,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ListDaemonsInput struct{}

type ListDaemonsOutput struct {
	Daemons []DaemonInfo `json:"daemons"`
	Error   string       `json:"error,omitempty"`
}

type ListDaemonSessionsInput struct {
	Daemon string `json:"daemon" jsonschema_description:"Daemon name from list_daemons"`
}

type ListDaemonSessionsOutput struct {
	Sessions []SessionInfo `json:"sessions"`
	Error    string        `json:"error,omitempty"`
}

type EnterDaemonInput struct {
	Daemon  string `json:"daemon" jsonschema_description:"Daemon name from list_daemons"`
	Session string `json:"session" jsonschema_description:"Session name from list_daemon_sessions"`
}

type EnterDaemonOutput struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type CreateSessionInput struct {
	Daemon string `json:"daemon" jsonschema_description:"Daemon name from list_daemons"`
	Name   string `json:"name" jsonschema_description:"Name for the new session"`
}

type CreateSessionOutput struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type RenameSessionInput struct {
	Daemon  string `json:"daemon" jsonschema_description:"Daemon name from list_daemons"`
	OldName string `json:"old_name" jsonschema_description:"Current session name"`
	NewName string `json:"new_name" jsonschema_description:"New session name"`
}

type RenameSessionOutput struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type KillSessionInput struct {
	Daemon string `json:"daemon" jsonschema_description:"Daemon name from list_daemons"`
	Name   string `json:"name" jsonschema_description:"Session name to kill"`
}

type KillSessionOutput struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type OpenTabOutput struct {
	Success bool   `json:"success"`
	TabID   string `json:"tab_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

type OpenSSHInput struct {
	Host string `json:"host" jsonschema_description:"Host name from list_hosts"`
}

type ListHostsOutput struct {
	Hosts []HostInfo `json:"hosts"`
	Error string     `json:"error,omitempty"`
}

type ListTmuxSessionsOutput struct {
	Sessions []SessionInfo `json:"sessions"`
	Error    string        `json:"error,omitempty"`
}

type OpenTmuxInput struct {
	Session string `json:"session" jsonschema_description:"Session name from list_tmux_sessions"`
}

// Tool handlers report executor failures in the output struct instead of
// returning a Go error: eino aborts the whole agent run on any tool error,
// and a failed operation (e.g. unknown session) is recoverable.
type toolBuilder struct {
	exec Executor
}

func BuildTools(exec Executor) ([]tool.BaseTool, error) {
	tb := &toolBuilder{exec: exec}

	listTabs, err := utils.InferTool("list_tabs", "List all open terminal tabs with their id, title, type and which one is active", tb.listTabs)
	if err != nil {
		return nil, fmt.Errorf("build list_tabs: %w", err)
	}
	readTab, err := utils.InferTool("read_tab", "Read a window of a tab's terminal transcript (scrollback + visible screen). Default shows the most recent content; increase skip_from_end to see EARLIER history, using total_bytes to know how far back you can page. History is bounded by the emulator's in-memory scrollback; older output is gone for good. Read before sending keys to see the current state", tb.readTab)
	if err != nil {
		return nil, fmt.Errorf("build read_tab: %w", err)
	}
	sendKeys, err := utils.InferTool("send_keys", "Inject keystrokes into a tab's pty and return the visible-screen tail after a short wait. Escape sequences are decoded before writing: \\\\ -> \\, \\n -> Enter, \\r -> CR, \\t -> Tab, \\xHH -> raw byte (\\x03 = Ctrl+C, \\x1b = Esc, \\x1b[A/B/C/D = arrows); to type a literal backslash use \\\\. Always read_tab first to confirm the prompt state", tb.sendKeys)
	if err != nil {
		return nil, fmt.Errorf("build send_keys: %w", err)
	}
	listDaemons, err := utils.InferTool("list_daemons", "List all registered remote daemons with their name, status and OS", tb.listDaemons)
	if err != nil {
		return nil, fmt.Errorf("build list_daemons: %w", err)
	}
	listDaemonSessions, err := utils.InferTool("list_daemon_sessions", "List the tmux sessions running on a remote daemon", tb.listDaemonSessions)
	if err != nil {
		return nil, fmt.Errorf("build list_daemon_sessions: %w", err)
	}
	enterDaemon, err := utils.InferTool("enter_daemon", "Open a shell tab attached to a session on a remote daemon", tb.enterDaemon)
	if err != nil {
		return nil, fmt.Errorf("build enter_daemon: %w", err)
	}
	createSession, err := utils.InferTool("create_session", "Create a new named tmux session on a remote daemon", tb.createSession)
	if err != nil {
		return nil, fmt.Errorf("build create_session: %w", err)
	}
	renameSession, err := utils.InferTool("rename_session", "Rename a tmux session on a remote daemon", tb.renameSession)
	if err != nil {
		return nil, fmt.Errorf("build rename_session: %w", err)
	}
	killSession, err := utils.InferTool("kill_session", "Kill a tmux session on a remote daemon. Destructive: the session and its running processes are lost", tb.killSession)
	if err != nil {
		return nil, fmt.Errorf("build kill_session: %w", err)
	}
	openLocal, err := utils.InferTool("open_local_terminal", "Open a new local shell tab on the user's machine and return the new tab id, ready for read_tab/send_keys", tb.openLocalTerminal)
	if err != nil {
		return nil, fmt.Errorf("build open_local_terminal: %w", err)
	}
	listHosts, err := utils.InferTool("list_hosts", "List the SSH hosts saved in the app with their name, address and tags", tb.listHosts)
	if err != nil {
		return nil, fmt.Errorf("build list_hosts: %w", err)
	}
	openSSH, err := utils.InferTool("open_ssh", "Open an SSH connection to a saved host (by name from list_hosts) in a new tab and return the new tab id. The connect runs asynchronously; the tool waits for the tab to appear, so a failure surfaces as a wait timeout", tb.openSSH)
	if err != nil {
		return nil, fmt.Errorf("build open_ssh: %w", err)
	}
	listTmux, err := utils.InferTool("list_tmux_sessions", "List the tmux sessions running on the user's local machine", tb.listTmuxSessions)
	if err != nil {
		return nil, fmt.Errorf("build list_tmux_sessions: %w", err)
	}
	openTmux, err := utils.InferTool("open_tmux", "Attach to a local tmux session (by name from list_tmux_sessions) in a new tab and return the new tab id", tb.openTmux)
	if err != nil {
		return nil, fmt.Errorf("build open_tmux: %w", err)
	}

	return []tool.BaseTool{listTabs, readTab, sendKeys, listDaemons, listDaemonSessions, enterDaemon, createSession, renameSession, killSession, openLocal, listHosts, openSSH, listTmux, openTmux}, nil
}

func (tb *toolBuilder) listTabs(ctx context.Context, in *ListTabsInput) (*ListTabsOutput, error) {
	tabs, err := tb.exec.ListTabs(ctx)
	if err != nil {
		return &ListTabsOutput{Error: err.Error()}, nil
	}
	return &ListTabsOutput{Tabs: tabs}, nil
}

func (tb *toolBuilder) readTab(ctx context.Context, in *ReadTabInput) (*ReadTabOutput, error) {
	maxBytes := in.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 8192
	}
	skip := in.SkipFromEnd
	if skip < 0 {
		skip = 0
	}
	content, total, err := tb.exec.ReadTab(ctx, in.ID, maxBytes, skip)
	if err != nil {
		return &ReadTabOutput{Error: err.Error()}, nil
	}
	return &ReadTabOutput{Content: content, TotalBytes: total}, nil
}

func (tb *toolBuilder) sendKeys(ctx context.Context, in *SendKeysInput) (*SendKeysOutput, error) {
	waitMs := in.WaitMs
	if waitMs <= 0 {
		waitMs = 300
	}
	screen, err := tb.exec.SendKeys(ctx, in.ID, in.Keys, waitMs)
	if err != nil {
		return &SendKeysOutput{Error: err.Error()}, nil
	}
	return &SendKeysOutput{Success: true, Screen: screen}, nil
}

func (tb *toolBuilder) listDaemons(ctx context.Context, in *ListDaemonsInput) (*ListDaemonsOutput, error) {
	daemons, err := tb.exec.ListDaemons(ctx)
	if err != nil {
		return &ListDaemonsOutput{Error: err.Error()}, nil
	}
	return &ListDaemonsOutput{Daemons: daemons}, nil
}

func (tb *toolBuilder) listDaemonSessions(ctx context.Context, in *ListDaemonSessionsInput) (*ListDaemonSessionsOutput, error) {
	sessions, err := tb.exec.ListDaemonSessions(ctx, in.Daemon)
	if err != nil {
		return &ListDaemonSessionsOutput{Error: err.Error()}, nil
	}
	return &ListDaemonSessionsOutput{Sessions: sessions}, nil
}

func (tb *toolBuilder) enterDaemon(ctx context.Context, in *EnterDaemonInput) (*EnterDaemonOutput, error) {
	if err := tb.exec.EnterDaemon(ctx, in.Daemon, in.Session); err != nil {
		return &EnterDaemonOutput{Error: err.Error()}, nil
	}
	return &EnterDaemonOutput{Success: true}, nil
}

func (tb *toolBuilder) createSession(ctx context.Context, in *CreateSessionInput) (*CreateSessionOutput, error) {
	if err := tb.exec.CreateSession(ctx, in.Daemon, in.Name); err != nil {
		return &CreateSessionOutput{Error: err.Error()}, nil
	}
	return &CreateSessionOutput{Success: true}, nil
}

func (tb *toolBuilder) renameSession(ctx context.Context, in *RenameSessionInput) (*RenameSessionOutput, error) {
	if err := tb.exec.RenameSession(ctx, in.Daemon, in.OldName, in.NewName); err != nil {
		return &RenameSessionOutput{Error: err.Error()}, nil
	}
	return &RenameSessionOutput{Success: true}, nil
}

func (tb *toolBuilder) killSession(ctx context.Context, in *KillSessionInput) (*KillSessionOutput, error) {
	if err := tb.exec.KillSession(ctx, in.Daemon, in.Name); err != nil {
		return &KillSessionOutput{Error: err.Error()}, nil
	}
	return &KillSessionOutput{Success: true}, nil
}

func (tb *toolBuilder) openLocalTerminal(ctx context.Context, in *ListTabsInput) (*OpenTabOutput, error) {
	id, err := tb.exec.OpenLocalTerminal(ctx)
	if err != nil {
		return &OpenTabOutput{Error: err.Error()}, nil
	}
	return &OpenTabOutput{Success: true, TabID: id}, nil
}

func (tb *toolBuilder) listHosts(ctx context.Context, in *ListTabsInput) (*ListHostsOutput, error) {
	hosts, err := tb.exec.ListHosts(ctx)
	if err != nil {
		return &ListHostsOutput{Error: err.Error()}, nil
	}
	return &ListHostsOutput{Hosts: hosts}, nil
}

func (tb *toolBuilder) openSSH(ctx context.Context, in *OpenSSHInput) (*OpenTabOutput, error) {
	id, err := tb.exec.OpenSSH(ctx, in.Host)
	if err != nil {
		return &OpenTabOutput{Error: err.Error()}, nil
	}
	return &OpenTabOutput{Success: true, TabID: id}, nil
}

func (tb *toolBuilder) listTmuxSessions(ctx context.Context, in *ListTabsInput) (*ListTmuxSessionsOutput, error) {
	sessions, err := tb.exec.ListTmuxSessions(ctx)
	if err != nil {
		return &ListTmuxSessionsOutput{Error: err.Error()}, nil
	}
	return &ListTmuxSessionsOutput{Sessions: sessions}, nil
}

func (tb *toolBuilder) openTmux(ctx context.Context, in *OpenTmuxInput) (*OpenTabOutput, error) {
	id, err := tb.exec.OpenTmux(ctx, in.Session)
	if err != nil {
		return &OpenTabOutput{Error: err.Error()}, nil
	}
	return &OpenTabOutput{Success: true, TabID: id}, nil
}
