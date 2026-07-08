package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/creack/pty"
	"github.com/google/uuid"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
)

const listFormat = "#{session_name}\t#{session_created}\t#{session_attached}"

var (
	runTmuxCmd        = runTmux
	attachTmuxSession = AttachSession
	killTmuxSession   = KillSession
)

func ListSessions(ctx context.Context) ([]types.TmuxSession, error) {
	cmd := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", listFormat)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if isNoServerOutput(out) {
			return nil, nil
		}
		return nil, tmuxCommandError("list-sessions", err, out)
	}
	return parseSessions(out), nil
}

func NewSession(ctx context.Context, rows, cols int) (*internalssh.InteractiveSession, string, error) {
	name := defaultSessionName()
	if err := runTmuxCmd(ctx, "new-session", newSessionDetachedArgs(name)); err != nil {
		return nil, "", err
	}
	is, err := attachTmuxSession(ctx, name, rows, cols)
	if err != nil {
		_ = killTmuxSession(ctx, name)
		return nil, "", err
	}
	return is, name, nil
}

func AttachSession(ctx context.Context, name string, rows, cols int) (*internalssh.InteractiveSession, error) {
	if err := runTmux(ctx, "set-option", statusOffArgs(name)); err != nil {
		return nil, err
	}
	cmd := exec.Command("tmux", attachSessionArgs(name)...)
	is, err := ptyCommand(cmd, rows, cols)
	if err != nil {
		return nil, tmuxCommandError("attach-session", err, nil)
	}
	return is, nil
}

func KillSession(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "tmux", "kill-session", "-t", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return tmuxCommandError("kill-session", err, out)
	}
	return nil
}

func RenameSession(ctx context.Context, oldName, newName string) error {
	cmd := exec.CommandContext(ctx, "tmux", "rename-session", "-t", oldName, newName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return tmuxCommandError("rename-session", err, out)
	}
	return nil
}

func defaultSessionName() string {
	return "tmux-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
}

func runTmux(ctx context.Context, op string, args []string) error {
	out, err := exec.CommandContext(ctx, "tmux", args...).CombinedOutput()
	if err != nil {
		return tmuxCommandError(op, err, out)
	}
	return nil
}

func newSessionDetachedArgs(name string) []string {
	return []string{"new-session", "-d", "-s", name}
}

func statusOffArgs(name string) []string {
	return []string{"set-option", "-t", name, "status", "off"}
}

func attachSessionArgs(name string) []string {
	return []string{"attach-session", "-t", name}
}

func parseSessions(out []byte) []types.TmuxSession {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	sessions := make([]types.TmuxSession, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		created, _ := strconv.ParseInt(parts[1], 10, 64)
		sessions = append(sessions, types.TmuxSession{
			Name:        parts[0],
			CreatedUnix: created,
			Attached:    parts[2] != "0",
		})
	}
	return sessions
}

func isNoServerOutput(out []byte) bool {
	text := string(out)
	return strings.Contains(text, "no server running") ||
		(strings.Contains(text, "error connecting to") && strings.Contains(text, "No such file or directory"))
}

func tmuxCommandError(op string, err error, out []byte) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("tmux not found in PATH")
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return fmt.Errorf("tmux %s: %w", op, err)
	}
	return fmt.Errorf("tmux %s: %w: %s", op, err, msg)
}

func ptyCommand(cmd *exec.Cmd, rows, cols int) (*internalssh.InteractiveSession, error) {
	rows, cols = internalssh.NormalizePTYSize(rows, cols)
	cmd.Env = internalssh.TerminalEnv(os.Environ())
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		done <- cmd.Wait()
		close(exited)
	}()
	is := &internalssh.InteractiveSession{
		Stdin:  f,
		Stdout: f,
		Done:   done,
		Resize: func(rows, cols int) error {
			return pty.Setsize(f, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
		},
	}
	is.AddCloser(internalssh.NewProcessExitCloser(exited, cmd.Process.Kill, internalssh.ProcessCloseKillTimeout))
	return is, nil
}
