package localterm

import (
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

const SettingShell = "local_terminal_shell"

func ResolveShell(configured string, exists func(string) bool) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured
	}
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	if exists("/bin/zsh") {
		return "/bin/zsh"
	}
	if exists("/bin/bash") {
		return "/bin/bash"
	}
	return "sh"
}

func DefaultShell(configured string) string {
	return ResolveShell(configured, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

func NewSession(shell string, rows, cols int) (*internalssh.InteractiveSession, error) {
	if cols < 40 {
		cols = 80
	}
	if rows < 5 {
		rows = 24
	}
	cmd := exec.Command(shell)
	cmd.Env = internalssh.TerminalEnv(os.Environ())
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return &internalssh.InteractiveSession{
		Stdin:  f,
		Stdout: f,
		Done:   done,
		Resize: func(rows, cols int) error {
			return pty.Setsize(f, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
		},
	}, nil
}
