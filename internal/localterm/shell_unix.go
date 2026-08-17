//go:build !windows

package localterm

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/huangzheng2016/eTerm/internal/shellintegr"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

func NewSession(shell string, rows, cols int) (*internalssh.InteractiveSession, error) {
	rows, cols = internalssh.NormalizePTYSize(rows, cols)
	args, env, _ := shellintegr.Wrap(shell)
	cmd := exec.Command(shell, args...)
	cmd.Env = append(internalssh.TerminalEnv(os.Environ()), env...)
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
