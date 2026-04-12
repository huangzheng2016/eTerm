// Package viewkeys provides per-view keybinding configuration passed from the app layer.
package viewkeys

// SFTPKeys holds configurable keybindings for the SFTP view.
type SFTPKeys struct {
	Upload     []string
	Download   []string
	Delete     []string
	Mkdir      []string
	Rename     []string
	SwitchLeft []string
	SwitchRight []string
}

// KeyViewKeys holds configurable keybindings for the SSH key management view.
type KeyViewKeys struct {
	New    []string
	Import []string
	Export []string
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
