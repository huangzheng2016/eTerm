package ssh

import (
	"strings"

	"golang.org/x/crypto/ssh"
)

const terminalTerm = "xterm-256color"
const terminalColorTerm = "truecolor"
const terminalTrueColor = "1"

func TerminalEnv(env []string) []string {
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		if strings.HasPrefix(kv, "TERM=") || strings.HasPrefix(kv, "COLORTERM=") || strings.HasPrefix(kv, "TRUECOLOR=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TERM="+terminalTerm, "COLORTERM="+terminalColorTerm, "TRUECOLOR="+terminalTrueColor)
}

func setTerminalEnv(sess *ssh.Session) {
	_ = sess.Setenv("TERM", terminalTerm)
	_ = sess.Setenv("COLORTERM", terminalColorTerm)
	_ = sess.Setenv("TRUECOLOR", terminalTrueColor)
}
