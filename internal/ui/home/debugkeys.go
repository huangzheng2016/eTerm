package home

import (
	"log"
	"os"

	tea "charm.land/bubbletea/v2"
)

func debugKeysEnabled() bool {
	return os.Getenv("ETERM_DEBUG_KEYS") != ""
}

// logKeyPress logs the raw key shape to stderr when ETERM_DEBUG_KEYS is set.
// Use this to see what the terminal actually sends when shortcuts fail.
func logKeyPress(context string, filterLabel string, itemCount int, msg tea.KeyPressMsg) {
	if !debugKeysEnabled() {
		return
	}
	k := msg.Key()
	log.Printf("[eterm:keys] %s filter=%s items=%d String=%q Keystroke=%q Code=U+%04X Text=%q Mod=%v",
		context, filterLabel, itemCount, k.String(), k.Keystroke(), k.Code, k.Text, k.Mod)
}

func logKeyNoShortcut(msg tea.KeyPressMsg) {
	if !debugKeysEnabled() {
		return
	}
	log.Printf("[eterm:keys] no home shortcut matched; passing to list (String=%q Keystroke=%q Code=U+%04X)",
		msg.Key().String(), msg.Key().Keystroke(), msg.Key().Code)
}

func logKeyDispatch(action string) {
	if !debugKeysEnabled() {
		return
	}
	log.Printf("[eterm:keys] dispatch %s", action)
}

func logKeyRepeatSuppressed(msg tea.KeyPressMsg) {
	if !debugKeysEnabled() {
		return
	}
	k := msg.Key()
	log.Printf("[eterm:keys] suppressing key-repeat for connect/SFTP (String=%q IsRepeat=%v)", k.String(), k.IsRepeat)
}
