package keymatch

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestMatchConnect_decoderCarriageReturn(t *testing.T) {
	var dec uv.EventDecoder
	n, ev := dec.Decode([]byte{'\r'})
	if n != 1 || ev == nil {
		t.Fatalf("decode \\r: n=%d ev=%v", n, ev)
	}
	kpe, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("expected KeyPressEvent, got %T", ev)
	}
	if !MatchConnect(tea.KeyPressMsg(tea.Key(kpe))) {
		t.Fatalf("decoder-produced key should match connect")
	}
}

func TestMatchSFTP_decoderPlainS(t *testing.T) {
	var dec uv.EventDecoder
	n, ev := dec.Decode([]byte{'s'})
	if n != 1 || ev == nil {
		t.Fatalf("decode s: n=%d ev=%v", n, ev)
	}
	kpe, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("expected KeyPressEvent, got %T", ev)
	}
	if !MatchSFTP(tea.KeyPressMsg(tea.Key(kpe))) {
		t.Fatalf("decoder-produced s should match SFTP")
	}
}
