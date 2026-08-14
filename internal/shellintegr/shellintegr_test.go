package shellintegr

import (
	"os"
	"os/exec"
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

// TestZshWrapperStartsClean runs a real zsh through the wrapper files: the
// user's .zshrc must be sourced exactly once and no recursion error may occur.
func TestZshWrapperStartsClean(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("echo USER_ZSHRC_RAN\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, env, ok := Wrap(zsh)
	if !ok {
		t.Fatal("expected zsh wrap")
	}
	var cleanEnv []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ZDOTDIR=") || strings.HasPrefix(kv, "ETERM_") {
			continue
		}
		cleanEnv = append(cleanEnv, kv)
	}
	cmd := exec.Command(zsh, "-i", "-c", "echo WRAPPER_OK; echo USER=$ETERM_USER_ZDOTDIR")
	cmd.Env = append(cleanEnv, env...)
	cmd.Dir = home
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh failed: %v\n%s", err, out)
	}
	s := string(out)
	if strings.Contains(s, "recursion") || strings.Contains(s, "job table full") {
		t.Fatalf("wrapper recursed:\n%s", s)
	}
	if strings.Count(s, "USER_ZSHRC_RAN") != 1 {
		t.Fatalf("user .zshrc not sourced exactly once:\n%s", s)
	}
	if !strings.Contains(s, "WRAPPER_OK") {
		t.Fatalf("shell did not run:\n%s", s)
	}
}
