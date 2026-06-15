package localterm

import "testing"

func TestResolveShellUsesConfiguredShell(t *testing.T) {
	got := ResolveShell("/opt/custom-shell", func(string) bool { return false })
	if got != "/opt/custom-shell" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveShellPrefersEnvThenZshThenBash(t *testing.T) {
	t.Setenv("SHELL", "/opt/env-shell")
	got := ResolveShell("", func(path string) bool {
		return path == "/bin/zsh" || path == "/bin/bash"
	})
	if got != "/opt/env-shell" {
		t.Fatalf("got %q", got)
	}

	t.Setenv("SHELL", "")
	got = ResolveShell("", func(path string) bool {
		return path == "/bin/zsh" || path == "/bin/bash"
	})
	if got != "/bin/zsh" {
		t.Fatalf("got %q", got)
	}

	got = ResolveShell("", func(path string) bool {
		return path == "/bin/bash"
	})
	if got != "/bin/bash" {
		t.Fatalf("got %q", got)
	}
}
