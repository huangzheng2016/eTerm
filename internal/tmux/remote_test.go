package tmux

import (
	"strings"
	"testing"
)

func TestRemoteAttachCommand(t *testing.T) {
	got := RemoteAttachCommand("/cfg/tmux.conf", "work")
	if got != "tmux -f '/cfg/tmux.conf' attach-session -t 'work'" {
		t.Fatalf("got %q", got)
	}
}

func TestRemoteAttachCommandNoConfig(t *testing.T) {
	got := RemoteAttachCommand("", "work")
	if got != "tmux attach-session -t 'work'" {
		t.Fatalf("got %q", got)
	}
}

func TestRemoteNewCommand(t *testing.T) {
	got := RemoteNewCommand(RemoteConfigPath, "eterm")
	if !strings.HasPrefix(got, "tmux -f $HOME/.config/eterm/tmux.conf new-session -A -s 'eterm'") {
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
