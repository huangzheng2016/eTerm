package ssh

import (
	"runtime"
	"strings"

	"golang.org/x/crypto/ssh"
)

const terminalTerm = "xterm-256color"
const terminalColorTerm = "truecolor"
const terminalTrueColor = "1"

func defaultLang(goos string) string {
	switch goos {
	case "darwin":
		return "en_US.UTF-8"
	case "linux":
		return "C.UTF-8"
	}
	return ""
}

func TerminalEnv(env []string) []string {
	out := make([]string, 0, len(env)+4)
	hasLocale := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "TERM=") || strings.HasPrefix(kv, "COLORTERM=") || strings.HasPrefix(kv, "TRUECOLOR=") {
			continue
		}
		if strings.HasPrefix(kv, "LC_ALL=") || strings.HasPrefix(kv, "LC_CTYPE=") || strings.HasPrefix(kv, "LANG=") {
			hasLocale = true
		}
		out = append(out, kv)
	}
	out = append(out, "TERM="+terminalTerm, "COLORTERM="+terminalColorTerm, "TRUECOLOR="+terminalTrueColor)
	if lang := defaultLang(runtime.GOOS); lang != "" && !hasLocale {
		out = append(out, "LANG="+lang)
	}
	return out
}

func setTerminalEnv(sess *ssh.Session) {
	_ = sess.Setenv("TERM", terminalTerm)
	_ = sess.Setenv("COLORTERM", terminalColorTerm)
	_ = sess.Setenv("TRUECOLOR", terminalTrueColor)
}
