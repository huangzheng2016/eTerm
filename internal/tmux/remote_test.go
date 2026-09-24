package tmux

import (
	"strings"
	"testing"
)

func TestRemoteAttachCommand(t *testing.T) {
	got := RemoteAttachCommand("/cfg/tmux.conf", "work")
	if got != "exec tmux -f '/cfg/tmux.conf' attach-session -t 'work'" {
		t.Fatalf("got %q", got)
	}
}

func TestRemoteAttachCommandNoConfig(t *testing.T) {
	got := RemoteAttachCommand("", "work")
	if got != "exec tmux attach-session -t 'work'" {
		t.Fatalf("got %q", got)
	}
}

func TestRemoteNewCommand(t *testing.T) {
	got := RemoteNewCommand(RemoteConfigPath, "eterm")
	if !strings.HasPrefix(got, "exec tmux -f $HOME/.config/eterm/tmux.conf new-session -A -s 'eterm'") {
		t.Fatalf("got %q", got)
	}
}

func TestQuoteShell(t *testing.T) {
	if got := quoteShell("a'b"); got != `'a'\''b'` {
		t.Fatalf("got %q", got)
	}
	if got := quoteShell("plain"); got != "'plain'" {
		t.Fatalf("got %q", got)
	}
}

func TestRemoteConfigArgTildeUnquoted(t *testing.T) {
	if got := remoteConfigArg("~/tmux.conf"); got != " -f ~/tmux.conf" {
		t.Fatalf("got %q", got)
	}
	if got := remoteConfigArg("/abs/path.conf"); got != " -f '/abs/path.conf'" {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureRemoteConfigUsesConfiguredPath(t *testing.T) {
	got, err := EnsureRemoteConfig(nil, "  /tmp/custom-tmux.conf  ")
	if err != nil {
		t.Fatalf("EnsureRemoteConfig returned error: %v", err)
	}
	if got != "/tmp/custom-tmux.conf" {
		t.Fatalf("got %q", got)
	}
}

func TestRemoteTempConfigCommand(t *testing.T) {
	got := remoteTempConfigCommand()
	for _, want := range []string{
		"mktemp /tmp/eterm-tmux.XXXXXXXX",
		`chmod 600 "$tmp"`,
		`cat > "$tmp"`,
		`printf '%s\n' "$tmp"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("command %q does not contain %q", got, want)
		}
	}
}
