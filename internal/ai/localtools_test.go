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

	// A non-zero exit is reported in the output, not as a Go error (eino
	// aborts the run on tool errors).
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

	// Relative path: StrReplaceEditor fails; the wrapper must not.
	out, err := editor.InvokableRun(context.Background(), `{"command":"view","path":"relative.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "error: ") {
		t.Fatalf("out = %q", out)
	}

	// Same for a wrapped stub failing outright.
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
	for _, s := range []string{"open_local_terminal", "open_ssh", "open_tmux", "list_hosts", "list_tmux_sessions", "notify"} {
		if !strings.Contains(on, s) {
			t.Fatalf("base prompt missing %q", s)
		}
	}
	if !strings.Contains(on, "list_daemons") || !strings.Contains(on, "kill_session") {
		t.Fatal("daemon section missing while enabled")
	}
	off := agentInstruction(false)
	if strings.Contains(off, "list_daemons") || strings.Contains(off, "kill_session") {
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
	for _, want := range []string{"open_local_terminal", "list_hosts", "open_ssh", "list_tmux_sessions", "open_tmux", "notify"} {
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
