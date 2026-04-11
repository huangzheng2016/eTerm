package keymatch

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func press(k tea.Key) tea.KeyPressMsg {
	return tea.KeyPressMsg(k)
}

func TestMatchConnect_enterTextCR(t *testing.T) {
	k := tea.Key{Code: tea.KeyEnter, Text: "\r"}
	if !MatchConnect(press(k)) {
		t.Fatalf("Enter with Text \\r should connect")
	}
}

func TestMatchConnect_enterPlain(t *testing.T) {
	k := tea.Key{Code: tea.KeyEnter}
	if !MatchConnect(press(k)) {
		t.Fatalf("plain KeyEnter should connect")
	}
}

func TestMatchConnect_enterCRLFText(t *testing.T) {
	k := tea.Key{Text: "\r\n"}
	if !MatchConnect(press(k)) {
		t.Fatalf("Text CRLF should connect when not hostile chord")
	}
}

func TestMatchConnect_ctrlEnterNo(t *testing.T) {
	k := tea.Key{Code: tea.KeyEnter, Mod: tea.ModCtrl}
	if MatchConnect(press(k)) {
		t.Fatalf("Ctrl+Enter should not be connect")
	}
}

func TestMatchSFTP_ctrlF(t *testing.T) {
	k := tea.Key{Code: 'f', Mod: tea.ModCtrl}
	if !MatchSFTP(press(k)) {
		t.Fatalf("Ctrl+F should open SFTP")
	}
}

func TestMatchSFTP_plainS(t *testing.T) {
	k := tea.Key{Code: 's', Text: "s"}
	if !MatchSFTP(press(k)) {
		t.Fatalf("plain s should open SFTP")
	}
}

func TestMatchSFTP_ctrlSNo(t *testing.T) {
	k := tea.Key{Code: 's', Mod: tea.ModCtrl, Text: "s"}
	// Keystroke for ctrl+s should be ctrl+s
	if MatchSFTP(press(k)) {
		t.Fatalf("Ctrl+S should not map to SFTP")
	}
}

func TestMatchConnect_baseCodeEnter(t *testing.T) {
	k := tea.Key{BaseCode: tea.KeyEnter, Code: 'x'}
	if !MatchConnect(press(k)) {
		t.Fatalf("BaseCode KeyEnter should connect")
	}
}

func TestMatchConnect_codeRuneCR(t *testing.T) {
	k := tea.Key{Code: '\r'}
	if !MatchConnect(press(k)) {
		t.Fatalf("Code rune \\r should connect")
	}
}

func TestMatchNewHostEditDeleteCopy(t *testing.T) {
	if !MatchNewHost(press(tea.Key{Code: 'n', Text: "n"})) {
		t.Fatal("n")
	}
	if !MatchEdit(press(tea.Key{Code: 'e', Text: "e"})) {
		t.Fatal("e")
	}
	if !MatchDelete(press(tea.Key{Code: 'd', Text: "d"})) {
		t.Fatal("d")
	}
	if !MatchCopy(press(tea.Key{Code: 'c', Text: "c"})) {
		t.Fatal("c")
	}
	if !MatchSearch(press(tea.Key{Code: '/', Text: "/"})) {
		t.Fatal("/")
	}
}

func TestMatchPlainLetters_ctrlNoMatch(t *testing.T) {
	ctrl := tea.ModCtrl
	if MatchEdit(press(tea.Key{Code: 'e', Text: "e", Mod: ctrl})) {
		t.Fatal("ctrl+e should not match edit")
	}
	if MatchSFTP(press(tea.Key{Code: 's', Text: "s", Mod: ctrl})) {
		t.Fatal("ctrl+s should not match SFTP")
	}
}
