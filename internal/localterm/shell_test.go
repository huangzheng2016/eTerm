package localterm

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

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

func TestNewSessionCloseKillsStubbornProcessAfterTimeout(t *testing.T) {
	oldTimeout := internalssh.ProcessCloseKillTimeout
	internalssh.ProcessCloseKillTimeout = 20 * time.Millisecond
	t.Cleanup(func() { internalssh.ProcessCloseKillTimeout = oldTimeout })
	shell := stubbornShell(t)

	is, err := NewSession(shell, 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	if err := is.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-is.Done:
	case <-time.After(time.Second):
		t.Fatal("process did not exit after close timeout")
	}
}

func stubbornShell(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stubborn-sh")
	script := "#!/bin/sh\ntrap '' HUP TERM INT\nwhile :; do :; done\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}
