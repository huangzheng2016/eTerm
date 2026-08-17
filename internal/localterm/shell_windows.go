//go:build windows

package localterm

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/UserExistsError/conpty"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

func NewSession(shell string, rows, cols int) (*internalssh.InteractiveSession, error) {
	rows, cols = internalssh.NormalizePTYSize(rows, cols)
	if !conpty.IsConPtyAvailable() {
		return nil, fmt.Errorf("ConPTY is not available on this Windows version")
	}
	// ConPty takes a command line, not argv; quote paths with spaces.
	cmdLine := shell
	if strings.Contains(shell, " ") && !strings.HasPrefix(shell, `"`) {
		cmdLine = `"` + shell + `"`
	}
	cpty, err := conpty.Start(cmdLine, conpty.ConPtyDimensions(cols, rows), conpty.ConPtyEnv(internalssh.TerminalEnv(os.Environ())))
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		code, err := cpty.Wait(context.Background())
		if err != nil {
			done <- err
			return
		}
		done <- fmt.Errorf("process exited with code %d", code)
	}()
	is := &internalssh.InteractiveSession{
		Stdin:  cpty,
		Stdout: cpty,
		Done:   done,
		Resize: func(rows, cols int) error {
			return cpty.Resize(cols, rows)
		},
	}
	// InteractiveSession.Close closes Stdin (= cpty): closing the pseudo
	// console terminates the attached shell. Do not also AddCloser(cpty);
	// ConPty.Close is not idempotent and a double close crashes natively.
	return is, nil
}
