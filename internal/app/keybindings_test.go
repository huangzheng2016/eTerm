package app

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func TestBuildKeyMapNewTabHelpUsesConfig(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	cfg.NewTab = []string{"ctrl+shift+t"}

	km := BuildKeyMap(cfg)

	if got := km.NewTab.Help().Key; got != "C-S-t" {
		t.Fatalf("got %q want C-S-t", got)
	}
}

func TestDefaultLocalTerminalKey(t *testing.T) {
	cfg := defaultKeyBindingConfig("linux")
	if len(cfg.LocalTerminal) != 1 || cfg.LocalTerminal[0] != "ctrl+shift+t" {
		t.Fatalf("LocalTerminal = %#v", cfg.LocalTerminal)
	}
}

func TestDefaultPasteImageURLKey(t *testing.T) {
	cfg := defaultKeyBindingConfig("linux")
	if len(cfg.PasteImageURL) != 1 || cfg.PasteImageURL[0] != "ctrl+shift+i" {
		t.Fatalf("PasteImageURL = %#v", cfg.PasteImageURL)
	}
}

func TestWindowsDefaultsAvoidCtrlShiftLetters(t *testing.T) {
	cfg := defaultKeyBindingConfig("windows")
	bindings := [][]string{
		cfg.QuitApp,
		cfg.CloseTabSafe,
		cfg.LockApp,
		cfg.ForwardTab,
		cfg.SnippetsTab,
		cfg.LocalTerminal,
		cfg.RenameTab,
		cfg.PasteImageURL,
		cfg.SnippetPicker,
		cfg.SessionHistory,
		cfg.BatchTag,
		cfg.BatchActions,
		cfg.SSHSnippetPicker,
	}
	for _, keys := range bindings {
		for _, key := range keys {
			if strings.HasPrefix(key, "ctrl+shift+") {
				t.Fatalf("Windows default still uses %q", key)
			}
		}
	}
	if cfg.CloseTabSafe[0] != "alt+shift+w" || cfg.RenameTab[0] != "alt+shift+r" {
		t.Fatalf("close=%v rename=%v", cfg.CloseTabSafe, cfg.RenameTab)
	}
}

func TestDefaultTabPageKeys(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	if len(cfg.TabPageLeft) != 1 || cfg.TabPageLeft[0] != "alt+shift+left" {
		t.Fatalf("TabPageLeft = %#v", cfg.TabPageLeft)
	}
	if len(cfg.TabPageRight) != 1 || cfg.TabPageRight[0] != "alt+shift+right" {
		t.Fatalf("TabPageRight = %#v", cfg.TabPageRight)
	}
}

func TestBuildKeyMapTabPageKeysUseConfig(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	cfg.TabPageLeft = []string{"alt+shift+up"}
	cfg.TabPageRight = []string{"alt+shift+down"}

	km := BuildKeyMap(cfg)
	if !key.Matches(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp, Mod: tea.ModAlt | tea.ModShift}), km.TabPageLeft) {
		t.Fatal("TabPageLeft does not use config")
	}
	if !key.Matches(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown, Mod: tea.ModAlt | tea.ModShift}), km.TabPageRight) {
		t.Fatal("TabPageRight does not use config")
	}
}

func TestDefaultAIAndPaletteKeys(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		cfg := defaultKeyBindingConfig(goos)
		if len(cfg.AIOverlay) != 1 || cfg.AIOverlay[0] != "ctrl+k" {
			t.Fatalf("%s AIOverlay = %#v", goos, cfg.AIOverlay)
		}
		if len(cfg.CommandPalette) != 1 || cfg.CommandPalette[0] != "ctrl+p" {
			t.Fatalf("%s CommandPalette = %#v", goos, cfg.CommandPalette)
		}
		// Global app-level bindings must not collide.
		globals := map[string][]string{
			"quit_app": cfg.QuitApp, "quit": cfg.Quit, "help": cfg.Help,
			"new_tab": cfg.NewTab, "close_tab": cfg.CloseTab, "close_tab_safe": cfg.CloseTabSafe,
			"next_tab": cfg.NextTab, "prev_tab": cfg.PrevTab,
			"tab_page_left": cfg.TabPageLeft, "tab_page_right": cfg.TabPageRight,
			"lock": cfg.Lock, "lock_app": cfg.LockApp,
			"forward_tab": cfg.ForwardTab, "snippets_tab": cfg.SnippetsTab,
			"command_palette": cfg.CommandPalette, "ai_overlay": cfg.AIOverlay,
			"local_terminal": cfg.LocalTerminal, "rename_tab": cfg.RenameTab,
			"paste_image_url": cfg.PasteImageURL,
		}
		seen := map[string]string{}
		for name, keys := range globals {
			for _, k := range keys {
				if prev, dup := seen[k]; dup {
					t.Fatalf("%s: %s and %s both use %q", goos, prev, name, k)
				}
				seen[k] = name
			}
		}
	}
}
