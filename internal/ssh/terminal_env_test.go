package ssh

import (
	"runtime"
	"strings"
	"testing"
)

func envValues(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

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

func TestTerminalEnvAddsLangWhenNoLocale(t *testing.T) {
	got := envValues(TerminalEnv([]string{"PATH=/bin", "USER=test"}))
	lang := defaultLang(runtime.GOOS)
	if lang == "" {
		if _, ok := got["LANG"]; ok {
			t.Fatalf("LANG = %q, want unset on %s", got["LANG"], runtime.GOOS)
		}
		return
	}
	if got["LANG"] != lang {
		t.Fatalf("LANG = %q, want %q", got["LANG"], lang)
	}
}

func TestTerminalEnvKeepsExistingLocale(t *testing.T) {
	for _, kv := range []string{"LC_ALL=zh_CN.UTF-8", "LC_CTYPE=zh_CN.UTF-8", "LANG=zh_CN.UTF-8"} {
		got := envValues(TerminalEnv([]string{"PATH=/bin", kv}))
		key := kv[:strings.IndexByte(kv, '=')]
		if got[key] != "zh_CN.UTF-8" {
			t.Fatalf("%s = %q, want zh_CN.UTF-8", key, got[key])
		}
		if key != "LANG" {
			if v, ok := got["LANG"]; ok {
				t.Fatalf("LANG = %q, want unset when %s is set", v, key)
			}
		}
	}
}

func TestDefaultLang(t *testing.T) {
	for goos, want := range map[string]string{
		"darwin":  "en_US.UTF-8",
		"linux":   "C.UTF-8",
		"windows": "",
	} {
		if got := defaultLang(goos); got != want {
			t.Fatalf("defaultLang(%q) = %q, want %q", goos, got, want)
		}
	}
}
