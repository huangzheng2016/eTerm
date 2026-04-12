package app

func (a App) activeTabIsSSH() bool {
	if a.viewState != MainView {
		return false
	}
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return false
	}
	return a.tabs[a.activeTab].Type == SSHTab
}

func (a App) activeTabIsEditor() bool {
	if a.viewState != MainView {
		return false
	}
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return false
	}
	return a.tabs[a.activeTab].Type == EditorTab
}

// nextTabOfType returns the index of the next tab of the given type after activeTab (wrapping).
// Returns -1 if no tab of that type exists.
func (a App) nextTabOfType(t TabType) int {
	n := len(a.tabs)
	for i := 1; i <= n; i++ {
		idx := (a.activeTab + i) % n
		if a.tabs[idx].Type == t {
			return idx
		}
	}
	return -1
}
