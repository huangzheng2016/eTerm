// Package viewkeys provides per-view keybinding configuration passed from the app layer.
package viewkeys

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// SFTPKeys holds configurable keybindings for the SFTP view.
type SFTPKeys struct {
	Upload      []string
	Download    []string
	Delete      []string
	Mkdir       []string
	Rename      []string
	Chmod       []string
	SwitchLeft  []string
	SwitchRight []string
}

// KeyViewKeys holds configurable keybindings for the SSH key management view.
type KeyViewKeys struct {
	New    []string
	Import []string
	Edit   []string
	Delete []string
	Copy   []string
}

// FwdKeys holds configurable keybindings for the port forward view.
type FwdKeys struct {
	Start  []string
	Stop   []string
	New    []string
	Edit   []string
	Delete []string
}

// SnippetKeys holds configurable keybindings for the snippet view.
type SnippetKeys struct {
	New    []string
	Edit   []string
	Delete []string
}

// SSHKeys holds configurable keybindings for the SSH terminal view.
type SSHKeys struct {
	Reconnect     []string
	SnippetPicker []string
}

// MatchAny checks if msg.String() matches any of the given keys.
func MatchAny(msgStr string, keys []string) bool {
	for _, k := range keys {
		if msgStr == k {
			return true
		}
	}
	return false
}

func MatchKey(msg tea.KeyPressMsg, keys []string) bool {
	k := msg.Key()
	msgStr := msg.String()
	ks := k.Keystroke()
	uk := uv.Key(k)
	for _, target := range keys {
		if target == "" {
			continue
		}
		if msgStr == target || ks == target || uk.MatchString(target) {
			return true
		}
		if matchCtrlShift(k, target) {
			return true
		}
	}
	return false
}

func HelpLabel(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		return shortKeyLabel(keys[0])
	}
	return shortKeyLabel(keys[0]) + "/" + shortKeyLabel(keys[1])
}

func shortKeyLabel(k string) string {
	parts := strings.Split(k, "+")
	for i, p := range parts {
		switch strings.ToLower(p) {
		case "ctrl":
			parts[i] = "C"
		case "shift":
			parts[i] = "S"
		case "alt":
			parts[i] = "A"
		case "meta":
			parts[i] = "M"
		default:
			parts[i] = p
		}
	}
	return strings.Join(parts, "-")
}

func matchCtrlShift(k tea.Key, target string) bool {
	const prefix = "ctrl+shift+"
	if !strings.HasPrefix(target, prefix) {
		return false
	}
	letter := []rune(strings.TrimPrefix(target, prefix))
	if len(letter) != 1 {
		return false
	}
	if !k.Mod.Contains(tea.ModCtrl) || !k.Mod.Contains(tea.ModShift) {
		return false
	}
	lower := unicode.ToLower(letter[0])
	upper := unicode.ToUpper(letter[0])
	return k.Code == lower || k.Code == upper || k.ShiftedCode == lower || k.ShiftedCode == upper
}
