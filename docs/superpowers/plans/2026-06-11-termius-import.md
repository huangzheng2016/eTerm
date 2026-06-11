# Termius Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Termius import flow behind the ESC menu Import item: pick source → select hosts → select keys (required keys locked) → resolve aliases → confirm → write to DB.

**Architecture:** Linear overlay stack stored as three `*model` fields on `App`. Overlays intercept key presses in `app_update.go` in reverse order (keyList → hostList → sourceMenu). DB logic lives in `import_termius.go`; each TUI overlay file handles only rendering and navigation state.

**Tech Stack:** Go, charm.land/bubbletea v2, charm.land/lipgloss v2, charm.land/bubbles v2/textinput, gorm, github.com/huangzheng2016/termius_exporter, golang.org/x/crypto/ssh

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/types/messages.go` | Modify | Add `OpenImportSourceMenuMsg` |
| `internal/app/escmenu.go` | Modify | Add `escMenuImport` item |
| `internal/app/overlay_mouse.go` | Modify | Add outside-click dismissal for three new overlays |
| `internal/app/app.go` | Modify | Add three new overlay fields to `App` struct |
| `internal/app/app_update.go` | Modify | Route keys + handle new message types |
| `internal/app/app_view.go` | Modify | Render new overlays |
| `internal/app/import_source_menu.go` | Create | Source picker overlay (Termius / ...) |
| `internal/app/import_termius.go` | Create | Internal message types, entry types, conflict detection, DB write |
| `internal/app/import_host_list.go` | Create | Step 1: host list with alias picker + rename sub-states |
| `internal/app/import_key_list.go` | Create | Step 2: key list with locked rows, alias picker, rename, confirm |
| `internal/app/import_termius_test.go` | Create | Unit tests for conflict detection + DB write |

---

## Task 1: Tag termius_exporter & add dependency to eTerm

**Files:**
- Modify: `go.mod`, `go.sum` (via `go get`)

- [ ] **Step 1: Tag the termius_exporter repo at v0.1.0**

```bash
cd ~/Temp/termius_export
git tag v0.1.0
git push origin v0.1.0
```

- [ ] **Step 2: Add the dependency to eTerm**

```bash
cd ~/Temp/eTerm
go get github.com/huangzheng2016/termius_exporter@v0.1.0
go mod tidy
```

Expected: `go.mod` now contains `github.com/huangzheng2016/termius_exporter v0.1.0`.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add termius_exporter v0.1.0"
```

---

## Task 2: Add OpenImportSourceMenuMsg to types

**Files:**
- Modify: `internal/types/messages.go`

- [ ] **Step 1: Add the message after `EscMenuRequestMsg`**

In `internal/types/messages.go`, after the line `type EscMenuRequestMsg struct{}`, add:

```go
// OpenImportSourceMenuMsg triggers the import source picker overlay.
type OpenImportSourceMenuMsg struct{}
```

- [ ] **Step 2: Build to confirm no errors**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/types/messages.go
git commit -m "types: add OpenImportSourceMenuMsg"
```

---

## Task 3: Update escmenu.go — add Import item

**Files:**
- Modify: `internal/app/escmenu.go`
- Modify: `internal/app/overlay_mouse.go` (mouse item count)

- [ ] **Step 1: Add `escMenuImport` constant between Settings and Sync**

Replace the const block in `internal/app/escmenu.go`:

```go
const (
	escMenuQuit escMenuItem = iota
	escMenuSettings
	escMenuImport
	escMenuSync
)
```

- [ ] **Step 2: Update `Update()` — add import case and extend down-bound**

Replace the `Update` method:

```go
func (m *escMenuModel) Update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < escMenuSync {
			m.cursor++
		}
	case "enter":
		switch m.cursor {
		case escMenuQuit:
			return true, func() tea.Msg { return types.QuitRequestMsg{} }
		case escMenuSettings:
			return true, func() tea.Msg { return types.OpenSettingsMsg{} }
		case escMenuImport:
			return true, func() tea.Msg { return types.OpenImportSourceMenuMsg{} }
		case escMenuSync:
			return true, func() tea.Msg { return types.OpenSyncMsg{} }
		}
	case "esc", "escape":
		return true, nil
	case "q":
		return true, func() tea.Msg { return types.QuitRequestMsg{} }
	case "s":
		return true, func() tea.Msg { return types.OpenSettingsMsg{} }
	case "i":
		return true, func() tea.Msg { return types.OpenImportSourceMenuMsg{} }
	case "y":
		return true, func() tea.Msg { return types.OpenSyncMsg{} }
	}
	return false, nil
}
```

- [ ] **Step 3: Update `View()` — add Import row**

Replace the `items` slice in `View()`:

```go
items := []struct {
    label string
    key   string
}{
    {"  Quit          ", "q"},
    {"  Settings      ", "s"},
    {"  Import        ", "i"},
    {"  Sync          ", "y"},
}
```

- [ ] **Step 4: Update mouse handler item count in overlay_mouse.go**

In `overlay_mouse.go`, in `escMenuMouse`, the check `itemY <= int(escMenuSync)` already works because `escMenuSync` is now 3. No change needed — confirm by reading:

```go
func (a App) escMenuMouse(lx, ly int) (tea.Model, tea.Cmd) {
	itemY := ly - 4
	if itemY >= 0 && itemY <= int(escMenuSync) {  // escMenuSync=3, still correct
```

- [ ] **Step 5: Build + commit**

```bash
go build ./...
git add internal/app/escmenu.go
git commit -m "feat: add Import item to ESC menu"
```

---

## Task 4: Create import_source_menu.go

**Files:**
- Create: `internal/app/import_source_menu.go`

- [ ] **Step 1: Write the file**

```go
package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type importSourceItem int

const (
	importSourceTermius importSourceItem = iota
)

type importSourceMenuModel struct {
	cursor importSourceItem
}

func newImportSourceMenu() *importSourceMenuModel {
	return &importSourceMenuModel{}
}

func (m *importSourceMenuModel) Update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < importSourceTermius {
			m.cursor++
		}
	case "enter":
		switch m.cursor {
		case importSourceTermius:
			return true, func() tea.Msg { return termiusLoadMsg{} }
		}
	case "t":
		return true, func() tea.Msg { return termiusLoadMsg{} }
	case "esc", "escape":
		return true, nil
	}
	return false, nil
}

func (m *importSourceMenuModel) View() string {
	items := []struct {
		label string
		key   string
	}{
		{"  Termius       ", "t"},
	}
	var rows string
	for i, item := range items {
		cursor := "  "
		style := ui.DimStyle
		if importSourceItem(i) == m.cursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		row := fmt.Sprintf("%s%s %s", cursor, style.Render(item.label), ui.DimStyle.Render("["+item.key+"]"))
		rows += row + "\n"
	}
	title := ui.TitleStyle.Render("Import from")
	hint1 := ui.DimStyle.Render("↑↓ navigate · enter select")
	hint2 := ui.DimStyle.Render("esc close")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", rows, hint1, hint2)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(40).
		Render(content)
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: fails with `termiusLoadMsg undefined` — that's correct, it's defined in Task 5.

- [ ] **Step 3: Commit after Task 5 compiles**

(defer commit to after Task 5)

---

## Task 5: Create import_termius.go — types, conflict detection, DB write

**Files:**
- Create: `internal/app/import_termius.go`
- Create: `internal/app/import_termius_test.go`

- [ ] **Step 1: Write the failing tests first**

```go
// internal/app/import_termius_test.go
package app

import (
	"path/filepath"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/termius_exporter/pkg/parser"
)

func TestBuildHostItems_ExactDuplicate(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	existing := db.Host{
		SyncID:     "h1",
		Alias:      "prod",
		Hostname:   "1.2.3.4",
		Port:       22,
		Username:   "root",
		AuthMethod: "agent",
	}
	database.Create(&existing)

	hosts := []parser.HostRecord{
		{Aliases: []string{"prod"}, Host: "1.2.3.4", Port: 22, Username: "root"},
	}
	items := buildHostItems(database, hosts)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if !items[0].blocked {
		t.Error("expected exact duplicate to be blocked")
	}
	if items[0].nameConflict {
		t.Error("exact duplicate should not be nameConflict")
	}
}

func TestBuildHostItems_NameConflict(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	existing := db.Host{
		SyncID:     "h2",
		Alias:      "prod",
		Hostname:   "9.9.9.9",
		Port:       22,
		Username:   "admin",
		AuthMethod: "agent",
	}
	database.Create(&existing)

	hosts := []parser.HostRecord{
		{Aliases: []string{"prod"}, Host: "1.2.3.4", Port: 22, Username: "root"},
	}
	items := buildHostItems(database, hosts)
	if items[0].blocked {
		t.Error("name conflict should not be blocked")
	}
	if !items[0].nameConflict {
		t.Error("expected nameConflict=true")
	}
}

func TestBuildHostItems_NoConflict(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	hosts := []parser.HostRecord{
		{Aliases: []string{"new-host"}, Host: "1.2.3.4", Port: 22, Username: "root"},
	}
	items := buildHostItems(database, hosts)
	if items[0].blocked || items[0].nameConflict {
		t.Error("expected no conflict for new host")
	}
}

func TestBuildKeyItems_ExactDuplicate(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Use a real ed25519 key fingerprint format
	database.Create(&db.SSHKey{
		SyncID:      "k1",
		Name:        "deploy",
		Type:        "ssh-ed25519",
		Fingerprint: "SHA256:AAAA",
	})
	keys := []parser.KeyRecord{
		{Aliases: []string{"deploy"}, PrivateKey: ""},
	}
	// Inject fingerprint directly for test
	items := buildKeyItemsWithFP(database, keys, []string{"SHA256:AAAA"})
	if !items[0].blocked {
		t.Error("expected exact duplicate key to be blocked")
	}
}

func TestBuildKeyItems_NameConflict(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.Create(&db.SSHKey{
		SyncID:      "k2",
		Name:        "deploy",
		Type:        "ssh-ed25519",
		Fingerprint: "SHA256:BBBB",
	})
	keys := []parser.KeyRecord{
		{Aliases: []string{"deploy"}, PrivateKey: ""},
	}
	items := buildKeyItemsWithFP(database, keys, []string{"SHA256:CCCC"})
	if items[0].blocked {
		t.Error("name conflict should not be blocked")
	}
	if !items[0].nameConflict {
		t.Error("expected nameConflict=true")
	}
}

func TestComputeKeyFingerprint_InvalidKey(t *testing.T) {
	fp := computeKeyFingerprint("not a valid key")
	if fp != "" {
		t.Errorf("expected empty fingerprint for invalid key, got %q", fp)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/app/... -run "TestBuild|TestCompute" -v 2>&1 | head -20
```

Expected: compile error (`buildHostItems undefined`).

- [ ] **Step 3: Write import_termius.go**

```go
package app

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/termius_exporter/pkg/exporter"
	"github.com/huangzheng2016/termius_exporter/pkg/parser"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

// Internal message types for the Termius import flow.
type termiusLoadMsg struct{}

type termiusExportResultMsg struct {
	hosts []parser.HostRecord
	keys  []parser.KeyRecord
	err   error
}

type termiusHostsReadyMsg struct {
	hostItems []importHostEntry
	// allKeys is read from a.importHostList.allKeys in app_update.go
}

type termiusImportRunMsg struct {
	hosts []importHostEntry
	keys  []importKeyEntry
}

type termiusImportResultMsg struct {
	imported int
	skipped  int
	err      error
}

// importHostEntry is one row in the host list overlay.
type importHostEntry struct {
	rec          parser.HostRecord
	selected     bool
	blocked      bool // exact duplicate — not selectable
	nameConflict bool // same alias, different content — must rename before import
	chosenAlias  string
}

// importKeyEntry is one row in the key list overlay.
type importKeyEntry struct {
	rec          parser.KeyRecord
	selected     bool
	blocked      bool // exact duplicate — not selectable
	locked       bool // required by a selected host — cannot deselect
	nameConflict bool // same name, different fingerprint — must rename
	chosenAlias  string
	fingerprint  string
}

func loadTermiusData() tea.Cmd {
	return func() tea.Msg {
		hosts, keys, err := exporter.Export("")
		return termiusExportResultMsg{hosts: hosts, keys: keys, err: err}
	}
}

// buildHostItems checks each HostRecord against the DB and returns importHostEntry rows.
func buildHostItems(database *gorm.DB, hosts []parser.HostRecord) []importHostEntry {
	items := make([]importHostEntry, 0, len(hosts))
	for _, h := range hosts {
		alias := ""
		if len(h.Aliases) > 0 {
			alias = h.Aliases[0]
		}
		var existing db.Host
		blocked := false
		nameConflict := false
		if err := database.Where("alias = ?", alias).First(&existing).Error; err == nil {
			if existing.Hostname == h.Host && existing.Port == h.Port && existing.Username == h.Username {
				blocked = true
			} else {
				nameConflict = true
			}
		}
		items = append(items, importHostEntry{
			rec:          h,
			blocked:      blocked,
			nameConflict: nameConflict,
			chosenAlias:  alias,
		})
	}
	return items
}

// buildKeyItems checks each KeyRecord against the DB and returns importKeyEntry rows.
func buildKeyItems(database *gorm.DB, keys []parser.KeyRecord) []importKeyEntry {
	fps := make([]string, len(keys))
	for i, k := range keys {
		fps[i] = computeKeyFingerprint(k.PrivateKey)
	}
	return buildKeyItemsWithFP(database, keys, fps)
}

// buildKeyItemsWithFP is the testable version that accepts pre-computed fingerprints.
func buildKeyItemsWithFP(database *gorm.DB, keys []parser.KeyRecord, fps []string) []importKeyEntry {
	items := make([]importKeyEntry, 0, len(keys))
	for i, k := range keys {
		alias := ""
		if len(k.Aliases) > 0 {
			alias = k.Aliases[0]
		}
		fp := fps[i]
		var existing db.SSHKey
		blocked := false
		nameConflict := false
		if err := database.Where("name = ?", alias).First(&existing).Error; err == nil {
			if existing.Fingerprint == fp && fp != "" {
				blocked = true
			} else {
				nameConflict = true
			}
		}
		items = append(items, importKeyEntry{
			rec:          k,
			blocked:      blocked,
			nameConflict: nameConflict,
			chosenAlias:  alias,
			fingerprint:  fp,
		})
	}
	return items
}

// lockRequiredKeys marks keys that are referenced by selected hosts as locked=true.
func lockRequiredKeys(hosts []importHostEntry, keys []importKeyEntry) []importKeyEntry {
	needed := make(map[string]bool)
	for _, h := range hosts {
		if h.selected && h.rec.KeyName != "" {
			needed[h.rec.KeyName] = true
		}
	}
	result := make([]importKeyEntry, len(keys))
	copy(result, keys)
	for i, k := range result {
		for _, alias := range k.rec.Aliases {
			if needed[alias] {
				result[i].locked = true
				result[i].selected = true
				break
			}
		}
	}
	return result
}

func computeKeyFingerprint(privateKeyPEM string) string {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return ""
	}
	h := sha256.Sum256(signer.PublicKey().Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:])
}

func runTermiusImport(database *gorm.DB, hosts []importHostEntry, keys []importKeyEntry) tea.Cmd {
	return func() tea.Msg {
		imported := 0
		skipped := 0
		// Maps chosen alias → DB key ID (for resolving host.KeyName after insert)
		keyAliasToID := make(map[string]uint)

		for _, ki := range keys {
			if ki.blocked || (!ki.selected && !ki.locked) {
				continue
			}
			signer, err := ssh.ParsePrivateKey([]byte(ki.rec.PrivateKey))
			if err != nil {
				skipped++
				continue
			}
			fp := ki.fingerprint
			if fp == "" {
				h := sha256.Sum256(signer.PublicKey().Marshal())
				fp = "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:])
			}
			k := db.SSHKey{
				SyncID:         fmt.Sprintf("termius-%s", ki.chosenAlias),
				Name:           ki.chosenAlias,
				Type:           signer.PublicKey().Type(),
				PrivateKeyData: ki.rec.PrivateKey,
				PublicKeyData:  string(ssh.MarshalAuthorizedKey(signer.PublicKey())),
				Fingerprint:    fp,
				StorageMode:    "database",
			}
			if err := database.Create(&k).Error; err != nil {
				skipped++
				continue
			}
			// Record under all original aliases and chosen alias for host resolution
			for _, a := range ki.rec.Aliases {
				keyAliasToID[a] = k.ID
			}
			keyAliasToID[ki.chosenAlias] = k.ID
			imported++
		}

		for _, hi := range hosts {
			if hi.blocked || !hi.selected {
				continue
			}
			var keyID *uint
			if hi.rec.KeyName != "" {
				if id, ok := keyAliasToID[hi.rec.KeyName]; ok {
					keyID = &id
				} else {
					var existingKey db.SSHKey
					if err := database.Where("name = ?", hi.rec.KeyName).First(&existingKey).Error; err == nil {
						id := existingKey.ID
						keyID = &id
					}
				}
			}
			authMethod := "agent"
			if keyID != nil {
				authMethod = "key"
			} else if hi.rec.Password != "" {
				authMethod = "password"
			}
			h := db.Host{
				SyncID:     fmt.Sprintf("termius-%s", hi.chosenAlias),
				Alias:      hi.chosenAlias,
				Hostname:   hi.rec.Host,
				Port:       hi.rec.Port,
				Username:   hi.rec.Username,
				AuthMethod: authMethod,
				KeyID:      keyID,
				Password:   hi.rec.Password,
			}
			if err := database.Create(&h).Error; err != nil {
				skipped++
				continue
			}
			imported++
		}
		return termiusImportResultMsg{imported: imported, skipped: skipped}
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/app/... -run "TestBuild|TestCompute" -v
```

Expected: all pass.

- [ ] **Step 5: Build everything**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/app/import_source_menu.go internal/app/import_termius.go internal/app/import_termius_test.go
git commit -m "feat: termius import types, conflict detection, and DB write logic"
```

---

## Task 6: Create import_host_list.go

**Files:**
- Create: `internal/app/import_host_list.go`

The host list has three sub-states:
- `hostListStateList` — main list; space=toggle, enter=open alias picker, y=proceed
- `hostListStateAlias` — alias picker for current item; enter=confirm, esc=back to list
- `hostListStateRename` — textinput; enter=confirm, esc=back to alias picker or list

- [ ] **Step 1: Write the file**

```go
package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type hostListState int

const (
	hostListStateList   hostListState = iota
	hostListStateAlias
	hostListStateRename
)

type importHostListModel struct {
	items          []importHostEntry
	cursor         int
	state          hostListState
	aliasCursor    int
	renameInput    textinput.Model
	renameFromAlias bool // true if we entered rename from the alias picker
}

func newImportHostList(items []importHostEntry) *importHostListModel {
	ti := textinput.New()
	ti.CharLimit = 128
	ti.SetWidth(40)
	return &importHostListModel{items: items, renameInput: ti}
}

// hasSelectableItems returns true if at least one non-blocked item exists.
func (m *importHostListModel) hasSelectableItems() bool {
	for _, it := range m.items {
		if !it.blocked {
			return true
		}
	}
	return false
}

// anySelected returns true if at least one item is selected.
func (m *importHostListModel) anySelected() bool {
	for _, it := range m.items {
		if it.selected {
			return true
		}
	}
	return false
}

// pendingConflict returns the first selected item that has nameConflict but
// chosenAlias still matches an existing alias (i.e. not yet renamed).
func (m *importHostListModel) pendingConflict() int {
	for i, it := range m.items {
		if it.selected && it.nameConflict {
			return i
		}
	}
	return -1
}

func (m *importHostListModel) Update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	switch m.state {
	case hostListStateList:
		return m.updateList(msg)
	case hostListStateAlias:
		return m.updateAlias(msg)
	case hostListStateRename:
		return m.updateRename(msg)
	}
	return false, nil
}

func (m *importHostListModel) updateList(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case " ":
		it := &m.items[m.cursor]
		if !it.blocked {
			it.selected = !it.selected
		}
	case "enter":
		it := &m.items[m.cursor]
		if it.blocked {
			break
		}
		if len(it.rec.Aliases) > 1 || it.nameConflict {
			m.aliasCursor = 0
			// Pre-position alias cursor on currently chosen alias
			for i, a := range it.rec.Aliases {
				if a == it.chosenAlias {
					m.aliasCursor = i
					break
				}
			}
			m.state = hostListStateAlias
		}
	case "y":
		if !m.anySelected() {
			break
		}
		// Force-resolve any pending conflicts first
		if idx := m.pendingConflict(); idx >= 0 {
			m.cursor = idx
			m.aliasCursor = 0
			m.state = hostListStateAlias
			break
		}
		return true, func() tea.Msg {
			return termiusHostsReadyMsg{hostItems: m.items}
		}
	case "esc", "escape":
		return true, nil
	}
	return false, nil
}

func (m *importHostListModel) updateAlias(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	it := &m.items[m.cursor]
	aliasCount := len(it.rec.Aliases) // +1 for "type new name" option
	totalOptions := aliasCount + 1

	switch msg.String() {
	case "up", "k":
		if m.aliasCursor > 0 {
			m.aliasCursor--
		}
	case "down", "j":
		if m.aliasCursor < totalOptions-1 {
			m.aliasCursor++
		}
	case "enter":
		if m.aliasCursor == aliasCount {
			// "type new name" option
			m.renameInput.SetValue(it.chosenAlias)
			m.renameInput.Focus()
			m.renameFromAlias = true
			m.state = hostListStateRename
		} else {
			it.chosenAlias = it.rec.Aliases[m.aliasCursor]
			it.nameConflict = false // resolved by picking a valid alias
			m.state = hostListStateList
		}
	case "esc", "escape":
		m.state = hostListStateList
	}
	return false, nil
}

func (m *importHostListModel) updateRename(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	it := &m.items[m.cursor]
	switch msg.String() {
	case "enter":
		val := strings.TrimSpace(m.renameInput.Value())
		if val == "" {
			break
		}
		it.chosenAlias = val
		it.nameConflict = false
		m.state = hostListStateList
	case "esc", "escape":
		if m.renameFromAlias {
			m.state = hostListStateAlias
		} else {
			m.state = hostListStateList
		}
	default:
		var tiCmd tea.Cmd
		m.renameInput, tiCmd = m.renameInput.Update(msg)
		return false, tiCmd
	}
	return false, nil
}

func (m *importHostListModel) View() string {
	switch m.state {
	case hostListStateAlias:
		return m.viewAlias()
	case hostListStateRename:
		return m.viewRename()
	}
	return m.viewList()
}

func (m *importHostListModel) viewList() string {
	title := ui.TitleStyle.Render("Import Hosts  (step 1/2)")

	var rows strings.Builder
	for i, it := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		check := "[ ]"
		rowStyle := ui.DimStyle
		if it.blocked {
			check = "[=]"
			rowStyle = ui.DimStyle
		} else if it.selected {
			check = "[x]"
			rowStyle = ui.SelectedStyle
		}

		label := it.chosenAlias
		endpoint := fmt.Sprintf("%s@%s:%d", it.rec.Username, it.rec.Host, it.rec.Port)
		line := fmt.Sprintf("%s%s %s  %s", cursor, check, rowStyle.Render(label), ui.DimStyle.Render(endpoint))

		if len(it.rec.Aliases) > 1 {
			line += ui.DimStyle.Render(fmt.Sprintf("  (%d aliases)", len(it.rec.Aliases)))
		}
		if it.rec.KeyName != "" {
			line += ui.DimStyle.Render(fmt.Sprintf("  [key: %s]", it.rec.KeyName))
		}
		if it.blocked {
			line += ui.DimStyle.Render("  [已存在]")
		}
		if it.nameConflict && it.selected {
			line += lipgloss.NewStyle().Foreground(lipgloss.Color("#F4A256")).Render("  [需改名]")
		}
		rows.WriteString(line + "\n")
	}

	hint := ui.DimStyle.Render("space 选择 · enter 别名 · y 下一步 · esc 返回")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", rows.String(), hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Width(64).
		Render(content)
}

func (m *importHostListModel) viewAlias() string {
	it := m.items[m.cursor]
	title := ui.TitleStyle.Render(fmt.Sprintf("选择别名: %s", it.chosenAlias))

	var rows strings.Builder
	for i, a := range it.rec.Aliases {
		cursor := "  "
		style := ui.DimStyle
		if i == m.aliasCursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		rows.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(a)))
	}
	// "type new name" option
	newNameOpt := len(it.rec.Aliases)
	cursor := "  "
	style := ui.DimStyle
	if m.aliasCursor == newNameOpt {
		cursor = "▸ "
		style = ui.SelectedStyle
	}
	rows.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render("[ 输入新名... ]")))

	hint := ui.DimStyle.Render("↑↓ · enter 确认 · esc 返回")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", rows.String(), hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(40).
		Render(content)
}

func (m *importHostListModel) viewRename() string {
	it := m.items[m.cursor]
	title := ui.TitleStyle.Render("输入新别名")
	msg := ui.DimStyle.Render(fmt.Sprintf("主机: %s@%s:%d", it.rec.Username, it.rec.Host, it.rec.Port))
	hint := ui.DimStyle.Render("enter 确认 · esc 返回")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", msg, "", m.renameInput.View(), "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(48).
		Render(content)
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/app/import_host_list.go
git commit -m "feat: termius import host list overlay"
```

---

## Task 7: Create import_key_list.go

**Files:**
- Create: `internal/app/import_key_list.go`

Mirrors host list but adds `locked` row handling and a `confirm` sub-state.

- [ ] **Step 1: Write the file**

```go
package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type keyListState int

const (
	keyListStateList    keyListState = iota
	keyListStateAlias
	keyListStateRename
	keyListStateConfirm
)

type importKeyListModel struct {
	items           []importKeyEntry
	hostItems       []importHostEntry
	cursor          int
	state           keyListState
	aliasCursor     int
	renameInput     textinput.Model
	renameFromAlias bool
}

func newImportKeyList(hosts []importHostEntry, keys []importKeyEntry) *importKeyListModel {
	ti := textinput.New()
	ti.CharLimit = 128
	ti.SetWidth(40)
	locked := lockRequiredKeys(hosts, keys)
	return &importKeyListModel{
		items:     locked,
		hostItems: hosts,
		renameInput: ti,
	}
}

func (m *importKeyListModel) pendingConflict() int {
	for i, it := range m.items {
		if (it.selected || it.locked) && it.nameConflict {
			return i
		}
	}
	return -1
}

func (m *importKeyListModel) Update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	switch m.state {
	case keyListStateList:
		return m.updateList(msg)
	case keyListStateAlias:
		return m.updateAlias(msg)
	case keyListStateRename:
		return m.updateRename(msg)
	case keyListStateConfirm:
		return m.updateConfirm(msg)
	}
	return false, nil
}

func (m *importKeyListModel) updateList(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case " ":
		it := &m.items[m.cursor]
		if !it.blocked && !it.locked {
			it.selected = !it.selected
		}
	case "enter":
		it := &m.items[m.cursor]
		if it.blocked {
			break
		}
		if len(it.rec.Aliases) > 1 || it.nameConflict {
			m.aliasCursor = 0
			for i, a := range it.rec.Aliases {
				if a == it.chosenAlias {
					m.aliasCursor = i
					break
				}
			}
			m.state = keyListStateAlias
		}
	case "y":
		if idx := m.pendingConflict(); idx >= 0 {
			m.cursor = idx
			m.aliasCursor = 0
			m.state = keyListStateAlias
			break
		}
		m.state = keyListStateConfirm
	case "esc", "escape":
		// Back to host list — the app handles this by nil-ing keyList
		return true, nil
	}
	return false, nil
}

func (m *importKeyListModel) updateAlias(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	it := &m.items[m.cursor]
	aliasCount := len(it.rec.Aliases)
	totalOptions := aliasCount + 1

	switch msg.String() {
	case "up", "k":
		if m.aliasCursor > 0 {
			m.aliasCursor--
		}
	case "down", "j":
		if m.aliasCursor < totalOptions-1 {
			m.aliasCursor++
		}
	case "enter":
		if m.aliasCursor == aliasCount {
			m.renameInput.SetValue(it.chosenAlias)
			m.renameInput.Focus()
			m.renameFromAlias = true
			m.state = keyListStateRename
		} else {
			it.chosenAlias = it.rec.Aliases[m.aliasCursor]
			it.nameConflict = false
			m.state = keyListStateList
		}
	case "esc", "escape":
		m.state = keyListStateList
	}
	return false, nil
}

func (m *importKeyListModel) updateRename(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	it := &m.items[m.cursor]
	switch msg.String() {
	case "enter":
		val := strings.TrimSpace(m.renameInput.Value())
		if val == "" {
			break
		}
		it.chosenAlias = val
		it.nameConflict = false
		m.state = keyListStateList
	case "esc", "escape":
		if m.renameFromAlias {
			m.state = keyListStateAlias
		} else {
			m.state = keyListStateList
		}
	default:
		var tiCmd tea.Cmd
		m.renameInput, tiCmd = m.renameInput.Update(msg)
		return false, tiCmd
	}
	return false, nil
}

func (m *importKeyListModel) updateConfirm(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "y":
		hosts := m.hostItems
		keys := m.items
		return true, func() tea.Msg { return termiusImportRunMsg{hosts: hosts, keys: keys} }
	case "n", "esc", "escape":
		m.state = keyListStateList
	}
	return false, nil
}

func (m *importKeyListModel) View() string {
	switch m.state {
	case keyListStateAlias:
		return m.viewAlias()
	case keyListStateRename:
		return m.viewRename()
	case keyListStateConfirm:
		return m.viewConfirm()
	}
	return m.viewList()
}

func (m *importKeyListModel) viewList() string {
	title := ui.TitleStyle.Render("Import Keys  (step 2/2)")

	var rows strings.Builder
	for i, it := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		check := "[ ]"
		rowStyle := ui.DimStyle
		switch {
		case it.blocked:
			check = "[=]"
		case it.locked:
			check = "[*]"
			rowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F4A256"))
		case it.selected:
			check = "[x]"
			rowStyle = ui.SelectedStyle
		}

		label := it.chosenAlias
		aliasHint := ""
		if len(it.rec.Aliases) > 1 {
			aliasHint = ui.DimStyle.Render(fmt.Sprintf("  (%d aliases)", len(it.rec.Aliases)))
		}
		line := fmt.Sprintf("%s%s %s%s", cursor, check, rowStyle.Render(label), aliasHint)
		if it.locked {
			line += ui.DimStyle.Render("  [必须]")
		}
		if it.blocked {
			line += ui.DimStyle.Render("  [已存在]")
		}
		if it.nameConflict && (it.selected || it.locked) {
			line += lipgloss.NewStyle().Foreground(lipgloss.Color("#F4A256")).Render("  [需改名]")
		}
		rows.WriteString(line + "\n")
	}

	hint := ui.DimStyle.Render("space 选择 · enter 别名 · y 确认导入 · esc 返回")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", rows.String(), hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Width(56).
		Render(content)
}

func (m *importKeyListModel) viewAlias() string {
	it := m.items[m.cursor]
	title := ui.TitleStyle.Render(fmt.Sprintf("选择别名: %s", it.chosenAlias))

	var rows strings.Builder
	for i, a := range it.rec.Aliases {
		cursor := "  "
		style := ui.DimStyle
		if i == m.aliasCursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		rows.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(a)))
	}
	newNameOpt := len(it.rec.Aliases)
	cursor := "  "
	style := ui.DimStyle
	if m.aliasCursor == newNameOpt {
		cursor = "▸ "
		style = ui.SelectedStyle
	}
	rows.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render("[ 输入新名... ]")))

	hint := ui.DimStyle.Render("↑↓ · enter 确认 · esc 返回")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", rows.String(), hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(40).
		Render(content)
}

func (m *importKeyListModel) viewRename() string {
	it := m.items[m.cursor]
	title := ui.TitleStyle.Render("输入新密钥名")
	msg := ui.DimStyle.Render(fmt.Sprintf("当前名: %s", it.chosenAlias))
	hint := ui.DimStyle.Render("enter 确认 · esc 返回")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", msg, "", m.renameInput.View(), "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(48).
		Render(content)
}

func (m *importKeyListModel) viewConfirm() string {
	selectedHosts := 0
	for _, h := range m.hostItems {
		if h.selected {
			selectedHosts++
		}
	}
	selectedKeys := 0
	lockedKeys := 0
	for _, k := range m.items {
		if k.locked {
			lockedKeys++
		} else if k.selected {
			selectedKeys++
		}
	}

	title := ui.TitleStyle.Render("导入确认")
	hostsLine := fmt.Sprintf("  主机: %d 个", selectedHosts)
	keysLine := fmt.Sprintf("  密钥: %d 个", selectedKeys+lockedKeys)
	if lockedKeys > 0 {
		keysLine += ui.DimStyle.Render(fmt.Sprintf(" (含 %d 个必须)", lockedKeys))
	}
	hint := ui.DimStyle.Render("  y 确认  n/esc 取消")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", hostsLine, keysLine, "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(40).
		Render(content)
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/app/import_key_list.go
git commit -m "feat: termius import key list overlay"
```

---

## Task 8: Wire overlays into App

**Files:**
- Modify: `internal/app/app.go` (add fields)
- Modify: `internal/app/app_update.go` (key routing + message handling)
- Modify: `internal/app/app_view.go` (rendering)
- Modify: `internal/app/overlay_mouse.go` (outside-click dismissal)

- [ ] **Step 1: Add three fields to App struct in app.go**

After the `importStratMenu` field, add:

```go
importSourceMenu *importSourceMenuModel
importHostList   *importHostListModel
importKeyList    *importKeyListModel
```

- [ ] **Step 2: Add key routing to app_update.go**

In the `KeyPressMsg` handler, before the `if a.importStratMenu != nil {` block, insert:

```go
if a.importKeyList != nil {
    closed, cmd := a.importKeyList.Update(msg)
    if closed {
        a.importKeyList = nil
    }
    return a, cmd
}

if a.importHostList != nil {
    closed, cmd := a.importHostList.Update(msg)
    if closed {
        a.importHostList = nil
        a.importSourceMenu = nil // full close on ESC from host list
    }
    return a, cmd
}

if a.importSourceMenu != nil {
    closed, cmd := a.importSourceMenu.Update(msg)
    if closed {
        a.importSourceMenu = nil
    }
    return a, cmd
}
```

- [ ] **Step 3: Add message handling to app_update.go**

In the main `switch msg.(type)` block, after the `case types.OpenSyncMsg:` case, add:

```go
case types.OpenImportSourceMenuMsg:
    a.escMenu = nil
    a.importSourceMenu = newImportSourceMenu()
    return a, nil

case termiusLoadMsg:
    a.importSourceMenu = nil
    return a, loadTermiusData()

case termiusExportResultMsg:
    if msg.err != nil {
        var tc tea.Cmd
        a.toast, tc = a.toast.Show(fmt.Sprintf("Termius import error: %v", msg.err), components.ToastError, 5*time.Second)
        return a, tea.Batch(tc, reflowWindow(a))
    }
    if len(msg.hosts) == 0 {
        var tc tea.Cmd
        a.toast, tc = a.toast.Show("Termius: no hosts found", components.ToastWarning, 3*time.Second)
        return a, tea.Batch(tc, reflowWindow(a))
    }
    hostItems := buildHostItems(a.db, msg.hosts)
    a.importHostList = newImportHostList(hostItems)
    a.importHostList.allKeys = msg.keys
    return a, nil

case termiusHostsReadyMsg:
    keyItems := buildKeyItems(a.db, a.importHostList.allKeys)
    a.importKeyList = newImportKeyList(msg.hostItems, keyItems)
    // Keep importHostList alive for back-navigation (ESC from key list re-shows host list)
    return a, nil

case termiusImportRunMsg:
    return a, runTermiusImport(a.db, msg.hosts, msg.keys)

case termiusImportResultMsg:
    a.importHostList = nil
    a.importKeyList = nil
    if msg.err != nil {
        var tc tea.Cmd
        a.toast, tc = a.toast.Show(fmt.Sprintf("Import failed: %v", msg.err), components.ToastError, 5*time.Second)
        return a, tea.Batch(tc, reflowWindow(a))
    }
    var tc tea.Cmd
    text := fmt.Sprintf("Imported %d items (%d skipped)", msg.imported, msg.skipped)
    a.toast, tc = a.toast.Show(text, components.ToastSuccess, 4*time.Second)
    return a, tea.Batch(tc, reflowWindow(a))
```

- [ ] **Step 4: Add `allKeys` field to importHostListModel in import_host_list.go**

In `import_host_list.go`, add to the struct and add the `parser` import:

```go
import "github.com/huangzheng2016/termius_exporter/pkg/parser"

type importHostListModel struct {
    items           []importHostEntry
    allKeys         []parser.KeyRecord  // passed through when opening key list step
    cursor          int
    state           hostListState
    aliasCursor     int
    renameInput     textinput.Model
    renameFromAlias bool
}
```

- [ ] **Step 5: Add rendering to app_view.go**

In `app_view.go`, in the overlay rendering chain, before `} else if a.importStratMenu != nil {`, add:

```go
} else if a.importKeyList != nil {
    overlay := a.importKeyList.View()
    main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
} else if a.importHostList != nil {
    overlay := a.importHostList.View()
    main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
} else if a.importSourceMenu != nil {
    overlay := a.importSourceMenu.View()
    main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
```

- [ ] **Step 6: Add outside-click dismissal in overlay_mouse.go**

In `handleOverlayMouse`, in the outside-click block where all overlays are nil'd, add:

```go
a.importKeyList = nil
a.importHostList = nil
a.importSourceMenu = nil
```

- [ ] **Step 7: Build everything**

```bash
go build ./...
```

Fix any compile errors (missing imports, field name mismatches).

- [ ] **Step 8: Run all tests**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/app/app.go internal/app/app_update.go internal/app/app_view.go internal/app/overlay_mouse.go internal/app/import_host_list.go
git commit -m "feat: wire Termius import overlays into app"
```

---

## Task 9: Smoke-test the full flow

**Files:** none (manual test)

- [ ] **Step 1: Run the app**

```bash
go run . 
```

- [ ] **Step 2: Test ESC menu shows Import**

Press `Esc`. Verify four items: Quit / Settings / Import / Sync.

- [ ] **Step 3: Test source menu**

Select Import. Verify "Import from" overlay with Termius item appears.

- [ ] **Step 4: Test Termius unavailable error**

If Termius is not installed/logged in, selecting Termius should show a toast error — not a crash.

- [ ] **Step 5: Commit final**

If any cosmetic fixes were made:

```bash
git add -A
git commit -m "fix: termius import smoke-test corrections"
```
