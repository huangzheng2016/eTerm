package shellintegr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrapUnsupportedAndDisabled(t *testing.T) {
	if _, _, ok := Wrap("sh"); ok {
		t.Fatal("expected sh to be unsupported")
	}
	t.Setenv(DisableEnv, "1")
	if _, _, ok := Wrap("/bin/zsh"); ok {
		t.Fatal("expected disabled integration")
	}
}

func TestWrapWritesWrappers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	args, env, ok := Wrap("/bin/zsh")
	if !ok {
		t.Fatal("expected zsh wrap")
	}
	if len(args) != 0 || len(env) != 2 || !strings.HasPrefix(env[0], "ZDOTDIR=") {
		t.Fatalf("args=%v env=%v", args, env)
	}
	zdir := strings.TrimPrefix(env[0], "ZDOTDIR=")
	for _, rel := range []string{".zshenv", ".zprofile", ".zshrc"} {
		if _, err := os.Stat(filepath.Join(zdir, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}

	args, _, ok = Wrap("/bin/bash")
	if !ok || len(args) != 2 || args[0] != "--rcfile" {
		t.Fatalf("bash args=%v ok=%v", args, ok)
	}
	if _, err := os.Stat(args[1]); err != nil {
		t.Fatalf("missing bash rcfile: %v", err)
	}

	args, _, ok = Wrap("/usr/bin/fish")
	if !ok || len(args) != 2 || args[0] != "-C" || !strings.HasPrefix(args[1], "source ") {
		t.Fatalf("fish args=%v ok=%v", args, ok)
	}
}

func TestTmuxCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("ZDOTDIR", "")

	cmd, ok := TmuxCommand()
	if !ok {
		t.Fatal("expected tmux command")
	}
	if !strings.Contains(cmd, "ZDOTDIR=") || !strings.Contains(cmd, "exec '/bin/zsh' -l") {
		t.Fatalf("cmd = %q", cmd)
	}

	t.Setenv("SHELL", "/bin/bash")
	cmd, ok = TmuxCommand()
	if !ok || !strings.Contains(cmd, "ETERM_SHELL_LOGIN=1") || !strings.Contains(cmd, "--rcfile") {
		t.Fatalf("cmd = %q ok = %v", cmd, ok)
	}

	t.Setenv("SHELL", "/bin/sh")
	if _, ok = TmuxCommand(); ok {
		t.Fatal("expected sh to be unsupported")
	}
}
