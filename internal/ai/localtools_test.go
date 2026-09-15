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
	names := map[string]bool{}
	for _, bt := range tools {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		names[info.Name] = true
	}
	for _, want := range []string{"open_local_terminal", "list_hosts", "open_ssh", "list_tmux_sessions", "open_tmux", "notify", "shell_history"} {
		if !names[want] {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	for _, unwanted := range []string{"list_daemons", "list_daemon_sessions", "enter_daemon", "create_session", "rename_session", "kill_session"} {
		if names[unwanted] {
			t.Fatalf("daemon tool %s present without daemons", unwanted)
		}
	}

	tools, err = BuildTools(fakeExecutor{}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	names = map[string]bool{}
	for _, bt := range tools {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		names[info.Name] = true
	}
	for _, want := range []string{"list_daemons", "list_daemon_sessions", "enter_daemon", "create_session", "rename_session", "kill_session"} {
		if !names[want] {
			t.Fatalf("missing daemon tool %s in %v", want, names)
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
	var notifyTool tool.BaseTool
	for _, bt := range tools {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == "notify" {
			notifyTool = bt
		}
	}
	if notifyTool == nil {
		t.Fatal("notify tool missing")
	}
	out, err := notifyTool.(tool.InvokableTool).InvokableRun(context.Background(), `{"text":"build finished"}`)
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

func TestReadShellHistoryZsh(t *testing.T) {
	home := t.TempDir()
	writeHistoryFile(t, home, ".zsh_history", ": 1694760000:0;git status\n: 1694760001:0;ls -la\nplain command\n")
	shell, path, cmds, err := ReadShellHistory(home, 50)
	if err != nil {
		t.Fatal(err)
	}
	if shell != "zsh" || path != filepath.Join(home, ".zsh_history") {
		t.Fatalf("shell=%q path=%q", shell, path)
	}
	want := []string{"plain command", "ls -la", "git status"}
	if strings.Join(cmds, "|") != strings.Join(want, "|") {
		t.Fatalf("cmds = %v, want %v", cmds, want)
	}
}

func TestReadShellHistoryLimit(t *testing.T) {
	home := t.TempDir()
	writeHistoryFile(t, home, ".zsh_history", "one\ntwo\nthree\nfour\n")
	_, _, cmds, err := ReadShellHistory(home, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 || cmds[0] != "four" || cmds[1] != "three" {
		t.Fatalf("cmds = %v", cmds)
	}
}

func TestReadShellHistoryBash(t *testing.T) {
	home := t.TempDir()
	writeHistoryFile(t, home, ".bash_history", "#1694760000\necho hi\n#1694760001\nmake build\n")
	shell, _, cmds, err := ReadShellHistory(home, 50)
	if err != nil {
		t.Fatal(err)
	}
	if shell != "bash" {
		t.Fatalf("shell = %q", shell)
	}
	if len(cmds) != 2 || cmds[0] != "make build" || cmds[1] != "echo hi" {
		t.Fatalf("cmds = %v", cmds)
	}
}

func TestReadShellHistoryFish(t *testing.T) {
	home := t.TempDir()
	writeHistoryFile(t, home, filepath.Join(".local", "share", "fish", "fish_history"), "- cmd: echo hi\n  when: 1694760000\n- cmd: cd /tmp\n  when: 1694760001\n  paths:\n    - /tmp\n")
	shell, _, cmds, err := ReadShellHistory(home, 50)
	if err != nil {
		t.Fatal(err)
	}
	if shell != "fish" {
		t.Fatalf("shell = %q", shell)
	}
	if len(cmds) != 2 || cmds[0] != "cd /tmp" || cmds[1] != "echo hi" {
		t.Fatalf("cmds = %v", cmds)
	}
}

func TestReadShellHistoryProbeOrder(t *testing.T) {
	home := t.TempDir()
	writeHistoryFile(t, home, ".zsh_history", "from zsh\n")
	writeHistoryFile(t, home, ".bash_history", "from bash\n")
	shell, _, cmds, err := ReadShellHistory(home, 50)
	if err != nil {
		t.Fatal(err)
	}
	if shell != "zsh" || len(cmds) != 1 || cmds[0] != "from zsh" {
		t.Fatalf("shell=%q cmds=%v", shell, cmds)
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
