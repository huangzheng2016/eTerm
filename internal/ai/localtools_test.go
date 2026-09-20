package ai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func invokable(t *testing.T, tools []tool.BaseTool, name string) tool.InvokableTool {
	t.Helper()
	for _, bt := range tools {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == name {
			it, ok := bt.(tool.InvokableTool)
			if !ok {
				t.Fatalf("%s is not invokable", name)
			}
			return it
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}

func toolsByName(t *testing.T, tools []tool.BaseTool) map[string]tool.BaseTool {
	t.Helper()
	byName := map[string]tool.BaseTool{}
	for _, bt := range tools {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byName[info.Name] = bt
	}
	return byName
}

func TestBashToolRunsCommand(t *testing.T) {
	tools, err := BuildLocalTools()
	if err != nil {
		t.Fatal(err)
	}
	bash := invokable(t, tools, "bash")

	out, err := bash.InvokableRun(context.Background(), `{"command":"echo hello-ai"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello-ai") || !strings.Contains(out, `"exit_code":0`) {
		t.Fatalf("out = %q", out)
	}

	out, err = bash.InvokableRun(context.Background(), `{"command":"echo oops >&2; exit 3"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"exit_code":3`) || !strings.Contains(out, "oops") {
		t.Fatalf("out = %q", out)
	}
}

func TestStrReplaceEditorOnLocalFS(t *testing.T) {
	tools, err := BuildLocalTools()
	if err != nil {
		t.Fatal(err)
	}
	editor := invokable(t, tools, "str_replace_editor")
	path := filepath.Join(t.TempDir(), "note.txt")

	out, err := editor.InvokableRun(context.Background(), `{"command":"create","path":"`+path+`","file_text":"alpha\nbeta\n"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("create out = %q", out)
	}

	out, err = editor.InvokableRun(context.Background(), `{"command":"str_replace","path":"`+path+`","old_str":"beta","new_str":"gamma"}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "alpha\ngamma\n" {
		t.Fatalf("file = %q, %v", data, err)
	}

	out, err = editor.InvokableRun(context.Background(), `{"command":"view","path":"`+path+`"}`)
	if err != nil || !strings.Contains(out, "gamma") {
		t.Fatalf("view out = %q, %v", out, err)
	}
}

func TestSafeToolConvertsErrorsToOutput(t *testing.T) {
	tools, err := BuildLocalTools()
	if err != nil {
		t.Fatal(err)
	}
	editor := invokable(t, tools, "str_replace_editor")

	out, err := editor.InvokableRun(context.Background(), `{"command":"view","path":"relative.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "error: ") {
		t.Fatalf("out = %q", out)
	}

	stub := &safeTool{inner: failingTool{}}
	out, err = stub.InvokableRun(context.Background(), `{}`)
	if err != nil || out != "error: boom" {
		t.Fatalf("out = %q, %v", out, err)
	}
}

type failingTool struct{}

func (failingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "failing"}, nil
}

func (failingTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "", errors.New("boom")
}

func TestAgentInstructionIncludesLocalTools(t *testing.T) {
	on := agentInstruction(true)
	if !strings.Contains(on, "str_replace_editor") || !strings.Contains(on, "bash:") {
		t.Fatal("local tools missing from the prompt")
	}
	for _, s := range []string{"open_local_terminal", "open_ssh", "open_tmux", "list_hosts", "list_tmux_sessions", "notify", "shell_history"} {
		if !strings.Contains(on, s) {
			t.Fatalf("base prompt missing %q", s)
		}
	}
	for _, s := range []string{"read-only tools first", "shell_history, list_tabs", "genuinely cannot be read"} {
		if !strings.Contains(on, s) {
			t.Fatalf("read-only guidance missing %q", s)
		}
	}
	if !strings.Contains(on, "list_daemons") || !strings.Contains(on, "kill_session") {
		t.Fatal("daemon section missing while enabled")
	}
	if !strings.Contains(on, "enter_daemon attaches an interactive tab") {
		t.Fatal("daemon read-only guidance missing while enabled")
	}
	off := agentInstruction(false)
	if strings.Contains(off, "list_daemons") || strings.Contains(off, "kill_session") || strings.Contains(off, "enter_daemon") {
		t.Fatal("daemon section present while disabled")
	}
	for _, s := range []string{"open_local_terminal", "str_replace_editor", "notify"} {
		if !strings.Contains(off, s) {
			t.Fatalf("non-daemon prompt missing %q", s)
		}
	}
}

func TestBuildToolsIncludesSessionOpenTools(t *testing.T) {
	tools, err := BuildTools(fakeExecutor{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	names := toolsByName(t, tools)
	for _, want := range []string{"open_local_terminal", "list_hosts", "open_ssh", "list_tmux_sessions", "open_tmux", "notify", "shell_history"} {
		if names[want] == nil {
			t.Fatalf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"list_daemons", "list_daemon_sessions", "enter_daemon", "create_session", "rename_session", "kill_session"} {
		if names[unwanted] != nil {
			t.Fatalf("daemon tool %s present without daemons", unwanted)
		}
	}

	tools, err = BuildTools(fakeExecutor{}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	names = toolsByName(t, tools)
	for _, want := range []string{"list_daemons", "list_daemon_sessions", "enter_daemon", "create_session", "rename_session", "kill_session"} {
		if names[want] == nil {
			t.Fatalf("missing daemon tool %s", want)
		}
	}
}

type notifyExecutor struct {
	Executor
	texts []string
}

func (e *notifyExecutor) Notify(ctx context.Context, text string) error {
	e.texts = append(e.texts, text)
	return nil
}

func TestNotifyToolCallsExecutor(t *testing.T) {
	exec := &notifyExecutor{}
	tools, err := BuildTools(exec, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	out, err := invokable(t, tools, "notify").InvokableRun(context.Background(), `{"text":"build finished"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "true") {
		t.Fatalf("out = %q", out)
	}
	if len(exec.texts) != 1 || exec.texts[0] != "build finished" {
		t.Fatalf("texts = %v", exec.texts)
	}
}

func TestTailRunes(t *testing.T) {
	if got := tailRunes("abc", 5); got != "abc" {
		t.Fatalf("got %q", got)
	}
	got := tailRunes("0123456789", 4)
	if !strings.HasPrefix(got, "<output clipped>") || !strings.HasSuffix(got, "6789") {
		t.Fatalf("got %q", got)
	}
}

func writeHistoryFile(t *testing.T, home, rel, content string) {
	t.Helper()
	p := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadShellHistory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		files     map[string]string
		limit     int
		wantShell string
		wantCmds  []string
	}{
		{
			name:      "zsh",
			files:     map[string]string{".zsh_history": ": 1694760000:0;git status\n: 1694760001:0;ls -la\nplain command\n"},
			limit:     50,
			wantShell: "zsh",
			wantCmds:  []string{"plain command", "ls -la", "git status"},
		},
		{
			name:      "limit keeps newest",
			files:     map[string]string{".zsh_history": "one\ntwo\nthree\nfour\n"},
			limit:     2,
			wantShell: "zsh",
			wantCmds:  []string{"four", "three"},
		},
		{
			name:      "bash",
			files:     map[string]string{".bash_history": "#1694760000\necho hi\n#1694760001\nmake build\n"},
			limit:     50,
			wantShell: "bash",
			wantCmds:  []string{"make build", "echo hi"},
		},
		{
			name:      "fish",
			files:     map[string]string{filepath.Join(".local", "share", "fish", "fish_history"): "- cmd: echo hi\n  when: 1694760000\n- cmd: cd /tmp\n  when: 1694760001\n  paths:\n    - /tmp\n"},
			limit:     50,
			wantShell: "fish",
			wantCmds:  []string{"cd /tmp", "echo hi"},
		},
		{
			name: "probe order prefers zsh",
			files: map[string]string{
				".zsh_history":  "from zsh\n",
				".bash_history": "from bash\n",
			},
			limit:     50,
			wantShell: "zsh",
			wantCmds:  []string{"from zsh"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			histFile := ""
			for rel, content := range tc.files {
				writeHistoryFile(t, home, rel, content)
				if len(tc.files) == 1 {
					histFile = rel
				}
			}
			shell, path, cmds, err := ReadShellHistory(home, tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			if shell != tc.wantShell {
				t.Fatalf("shell = %q, want %q", shell, tc.wantShell)
			}
			if histFile != "" && path != filepath.Join(home, histFile) {
				t.Fatalf("path = %q", path)
			}
			if strings.Join(cmds, "|") != strings.Join(tc.wantCmds, "|") {
				t.Fatalf("cmds = %v, want %v", cmds, tc.wantCmds)
			}
		})
	}
}

func TestReadShellHistoryMissing(t *testing.T) {
	_, _, _, err := ReadShellHistory(t.TempDir(), 50)
	if err == nil || !strings.Contains(err.Error(), "no shell history file found") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadShellHistoryUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	home := t.TempDir()
	writeHistoryFile(t, home, ".zsh_history", "hidden command\n")
	p := filepath.Join(home, ".zsh_history")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	_, _, _, err := ReadShellHistory(home, 50)
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("err = %v", err)
	}
}

type historyExecutor struct {
	Executor
	limit int
	shell string
	path  string
	cmds  []string
	err   error
}

func (e *historyExecutor) ShellHistory(_ context.Context, limit int) (string, string, []string, error) {
	e.limit = limit
	return e.shell, e.path, e.cmds, e.err
}

func TestShellHistoryTool(t *testing.T) {
	exec := &historyExecutor{shell: "zsh", path: "/home/u/.zsh_history", cmds: []string{"ls", "pwd"}}
	tools, err := BuildTools(exec, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	sh := invokable(t, tools, "shell_history")
	out, err := sh.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if exec.limit != 50 {
		t.Fatalf("default limit = %d, want 50", exec.limit)
	}
	if !strings.Contains(out, `"zsh"`) || !strings.Contains(out, "ls") || !strings.Contains(out, "/home/u/.zsh_history") {
		t.Fatalf("out = %q", out)
	}

	exec.err = errors.New("no shell history file found")
	out, err = sh.InvokableRun(context.Background(), `{"limit":10}`)
	if err != nil {
		t.Fatal(err)
	}
	if exec.limit != 10 {
		t.Fatalf("limit = %d, want 10", exec.limit)
	}
	if !strings.Contains(out, "no shell history file found") {
		t.Fatalf("out = %q", out)
	}
}
