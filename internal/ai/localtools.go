package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/tool/commandline"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

const (
	localCommandTimeout = 120 * time.Second
	localOutputMaxRunes = 16000
)

type localOperator struct{}

func (localOperator) ReadFile(_ context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

func (localOperator) WriteFile(_ context.Context, path string, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func (localOperator) IsDirectory(_ context.Context, path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func (localOperator) Exists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (localOperator) RunCommand(ctx context.Context, command []string) (*commandline.CommandOutput, error) {
	if len(command) == 0 {
		return nil, errors.New("empty command")
	}
	ctx, cancel := context.WithTimeout(ctx, localCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := &commandline.CommandOutput{
		Stdout: tailRunes(stdout.String(), localOutputMaxRunes),
		Stderr: tailRunes(stderr.String(), localOutputMaxRunes),
	}
	if ctx.Err() == context.DeadlineExceeded {
		out.ExitCode = -1
		out.Stderr += fmt.Sprintf("\ncommand killed after %s", localCommandTimeout)
		return out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		out.ExitCode = exitErr.ExitCode()
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func localShell() string {
	for _, sh := range []string{"bash", "sh"} {
		if p, err := exec.LookPath(sh); err == nil {
			return p
		}
	}
	return ""
}

func tailRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return "<output clipped>\n" + string(runes[len(runes)-max:])
}

type BashInput struct {
	Command string `json:"command" jsonschema_description:"Shell command to run on the user's local machine"`
}

type BashOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

type safeTool struct {
	mu    sync.Mutex
	inner tool.InvokableTool
}

func (s *safeTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return s.inner.Info(ctx)
}

func (s *safeTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.inner.InvokableRun(ctx, argumentsInJSON, opts...)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	return out, nil
}

func BuildLocalTools() ([]tool.BaseTool, error) {
	op := localOperator{}
	run := func(ctx context.Context, in *BashInput) (*BashOutput, error) {
		shell := localShell()
		if shell == "" {
			return &BashOutput{Error: "no shell found (looked for bash, sh)"}, nil
		}
		out, err := op.RunCommand(ctx, []string{shell, "-c", in.Command})
		if err != nil {
			return &BashOutput{Error: err.Error()}, nil
		}
		return &BashOutput{Stdout: out.Stdout, Stderr: out.Stderr, ExitCode: out.ExitCode}, nil
	}
	bashTool, err := utils.InferTool("bash", "Run a shell command on the user's local machine via bash -c (unsandboxed, full user privileges) and return stdout, stderr and the exit code. A non-zero exit code is a normal result, not a tool failure. 120s timeout; long output is clipped to the last 16000 chars", run)
	if err != nil {
		return nil, fmt.Errorf("build bash: %w", err)
	}
	editor, err := commandline.NewStrReplaceEditor(context.Background(), &commandline.EditorConfig{Operator: op})
	if err != nil {
		return nil, fmt.Errorf("build str_replace_editor: %w", err)
	}
	return []tool.BaseTool{bashTool, &safeTool{inner: editor}}, nil
}
