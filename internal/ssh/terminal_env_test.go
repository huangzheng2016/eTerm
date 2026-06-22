package ssh

import "testing"

func TestTerminalEnvAdvertisesTrueColor(t *testing.T) {
	env := TerminalEnv([]string{
		"PATH=/bin",
		"TERM=vt100",
		"COLORTERM=",
		"TRUECOLOR=0",
		"USER=test",
	})

	want := map[string]string{
		"PATH":      "/bin",
		"USER":      "test",
		"TERM":      "xterm-256color",
		"COLORTERM": "truecolor",
		"TRUECOLOR": "1",
	}
	got := map[string]string{}
	for _, kv := range env {
		for i, r := range kv {
			if r == '=' {
				got[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q want %q", k, got[k], v)
		}
	}
}
