package home

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/keymatch"
	"github.com/eterm/eterm/internal/security"
	"gorm.io/gorm"
)

type hostItem struct {
	host db.Host
}

// displayGroupName is the label shown in the list; blank DB group means "Default".
func displayGroupName(g string) string {
	if strings.TrimSpace(g) == "" {
		return "Default"
	}
	return g
}

func (i hostItem) FilterValue() string {
	// Must start with the same prefix as Title() so bubbles' filter highlight
	// underlines the correct characters.
	prefix := "[" + displayGroupName(i.host.Group) + "] "
	name := i.host.Alias
	if name == "" {
		name = fmt.Sprintf("%s@%s", i.host.Username, i.host.Hostname)
	}
	return prefix + name + " " + i.host.Hostname + " " + i.host.Tags
}

func (i hostItem) Title() string {
	prefix := "[" + displayGroupName(i.host.Group) + "] "
	if i.host.Alias != "" {
		return prefix + i.host.Alias
	}
	return prefix + fmt.Sprintf("%s@%s", i.host.Username, i.host.Hostname)
}

func (i hostItem) Description() string {
	return fmt.Sprintf("%s@%s:%d [%s] %s", i.host.Username, i.host.Hostname, i.host.Port, i.host.AuthMethod, i.host.Tags)
}

type hostsLoadedMsg struct {
	hosts []db.Host
	err   error
}

type viewMode int

const (
	groupView viewMode = iota
	tagView
)

type tagItem struct {
	name  string
	count int // number of hosts with this tag
}

func (t tagItem) FilterValue() string { return t.name }
func (t tagItem) Title() string       { return t.name }
func (t tagItem) Description() string { return fmt.Sprintf("%d hosts", t.count) }

type Model struct {
	list         list.Model
	keys         listKeyMap
	db           *gorm.DB
	masterKey    *security.MasterKeyManager
	width        int
	height       int
	loaded       bool
	lastClickAt  time.Time
	lastClickIdx int // global visible index; -1 if none

	// Grid state
	gridCursor int
	gridLayout gridLayout

	// Tag view state
	mode        viewMode
	allHosts    []db.Host  // cached hosts for tag filtering
	allTags     []string   // deduplicated sorted tags
	selectedTag string     // current tag filter; empty = show tag list
	tagList     list.Model // list of tags to pick from

	// Hidden host toggle
	showHidden bool

	// Host online status (from TCP probe)
	hostStatus map[uint]HostStatus

	// Configurable keymatch config
	kmCfg keymatch.Config

	// Configurable home-specific keys
	quickConnectKeys   []string
	importSSHKeys      []string
	exportConfigKeys   []string
	showHiddenKeys     []string
	hideHostKeys       []string
	sessionHistoryKeys []string
	toggleSelectKeys   []string
	batchTagKeys       []string

	// Multi-select (grid) for batch tag
	selectedHosts map[uint]struct{}

	// Grid status line: ON/OFF/? text next to probe dot (opt-in via app_settings grid_status_words).
	gridStatusWords bool
}

// HomeKeyConfig holds all configurable keys for the home view.
type HomeKeyConfig struct {
	KmCfg          keymatch.Config
	Keys           listKeyMap
	QuickConnect   []string
	ImportSSH      []string
	ExportConfig   []string
	ShowHidden     []string
	HideHost       []string
	SessionHistory []string
	ToggleSelect   []string
	BatchTag       []string
}

func New(database *gorm.DB, masterKey *security.MasterKeyManager, hkc HomeKeyConfig) Model {
	delegate := list.NewDefaultDelegate()
	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetFilteringEnabled(true)
	// Built-in list help off — global shortcuts are on the app status bar; ? opens full overlay
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.KeyMap.Quit.SetEnabled(false)
	l.KeyMap.ForceQuit.SetEnabled(false)
	// Remove conflicting bindings: d/f are used by our own handlers
	l.KeyMap.NextPage = key.NewBinding(key.WithKeys("pgdown"))
	l.KeyMap.PrevPage = key.NewBinding(key.WithKeys("pgup"))
	return Model{
		list:               l,
		keys:               hkc.Keys,
		db:                 database,
		masterKey:          masterKey,
		lastClickIdx:       -1,
		tagList:            newTagList(),
		kmCfg:              hkc.KmCfg,
		quickConnectKeys:   hkc.QuickConnect,
		importSSHKeys:      hkc.ImportSSH,
		exportConfigKeys:   hkc.ExportConfig,
		showHiddenKeys:     hkc.ShowHidden,
		hideHostKeys:       hkc.HideHost,
		sessionHistoryKeys: hkc.SessionHistory,
		toggleSelectKeys:   hkc.ToggleSelect,
		batchTagKeys:       hkc.BatchTag,
		selectedHosts:      make(map[uint]struct{}),
	}
}

// WithUpdatedKeys returns a copy of the model with updated keybinding configuration.
func (m Model) WithUpdatedKeys(hkc HomeKeyConfig) Model {
	m.keys = hkc.Keys
	m.kmCfg = hkc.KmCfg
	m.quickConnectKeys = hkc.QuickConnect
	m.importSSHKeys = hkc.ImportSSH
	m.exportConfigKeys = hkc.ExportConfig
	m.showHiddenKeys = hkc.ShowHidden
	m.hideHostKeys = hkc.HideHost
	m.sessionHistoryKeys = hkc.SessionHistory
	m.toggleSelectKeys = hkc.ToggleSelect
	m.batchTagKeys = hkc.BatchTag
	return m
}

func (m *Model) SetSize(w, h int) {
	if w < 20 {
		w = 80
	}
	m.width = w
	m.height = h
	m.resizeList()
}

func (m *Model) resizeList() {
	if m.height < 1 {
		m.height = 1
	}
	m.list.SetSize(m.width, m.height)
	m.tagList.SetSize(m.width, m.height)
	m.gridLayout = computeGrid(m.width, m.height)
}

// gridHosts returns the hosts currently visible (respects search filter).
func (m Model) gridHosts() []db.Host {
	vis := m.list.VisibleItems()
	hosts := make([]db.Host, 0, len(vis))
	for _, it := range vis {
		if hi, ok := it.(hostItem); ok {
			hosts = append(hosts, hi.host)
		}
	}
	return hosts
}

func (m Model) SelectedHost() *db.Host {
	hosts := m.gridHosts()
	if len(hosts) == 0 {
		return nil
	}
	idx := m.gridCursor
	if idx < 0 || idx >= len(hosts) {
		idx = 0
	}
	return &hosts[idx]
}

func readGridStatusWords(database *gorm.DB) bool {
	s, err := db.GetSetting(database, "grid_status_words")
	if err != nil {
		return false
	}
	return s == "true"
}

func (m Model) batchHostIDs() []uint {
	if len(m.selectedHosts) > 0 {
		out := make([]uint, 0, len(m.selectedHosts))
		for id := range m.selectedHosts {
			out = append(out, id)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	if h := m.SelectedHost(); h != nil {
		return []uint{h.ID}
	}
	return nil
}

func (m Model) loadHosts() tea.Cmd {
	return func() tea.Msg {
		var hosts []db.Host
		err := m.db.Order("last_connected_at DESC NULLS LAST, alias ASC").Find(&hosts).Error
		return hostsLoadedMsg{hosts: hosts, err: err}
	}
}

func newTagList() list.Model {
	delegate := list.NewDefaultDelegate()
	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.KeyMap.Quit.SetEnabled(false)
	l.KeyMap.ForceQuit.SetEnabled(false)
	l.KeyMap.NextPage = key.NewBinding(key.WithKeys("pgdown"))
	l.KeyMap.PrevPage = key.NewBinding(key.WithKeys("pgup"))
	return l
}

func parseTags(tags string) []string {
	if strings.TrimSpace(tags) == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	var result []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}

func collectAllTags(hosts []db.Host) []string {
	seen := map[string]bool{}
	for _, h := range hosts {
		for _, t := range parseTags(h.Tags) {
			if strings.EqualFold(t, "hidden") {
				continue // hidden is a special tag, not shown in tag list
			}
			seen[t] = true
		}
	}
	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

func hostHasTag(h db.Host, tag string) bool {
	for _, t := range parseTags(h.Tags) {
		if t == tag {
			return true
		}
	}
	return false
}

func tagCounts(hosts []db.Host) map[string]int {
	counts := map[string]int{}
	for _, h := range hosts {
		for _, t := range parseTags(h.Tags) {
			counts[t]++
		}
	}
	return counts
}

// filterHidden removes hosts with the "hidden" tag unless showHidden is true.
func (m *Model) filterHidden(hosts []db.Host) []db.Host {
	if m.showHidden {
		return hosts
	}
	out := make([]db.Host, 0, len(hosts))
	for _, h := range hosts {
		if !hostHasTag(h, "hidden") {
			out = append(out, h)
		}
	}
	return out
}

// populateHostList fills the host list with the given hosts.
func (m *Model) populateHostList(hosts []db.Host) {
	hosts = m.filterHidden(hosts)
	items := make([]list.Item, len(hosts))
	for i, h := range hosts {
		items[i] = hostItem{host: h}
	}
	m.list.SetItems(items)
	if len(items) > 0 {
		m.list.Select(0)
	}
	m.gridCursor = 0
}

// populateTagList fills the tag list from allHosts.
func (m *Model) populateTagList() {
	counts := tagCounts(m.allHosts)
	items := make([]list.Item, len(m.allTags))
	for i, t := range m.allTags {
		items[i] = tagItem{name: t, count: counts[t]}
	}
	m.tagList.SetItems(items)
	if len(items) > 0 {
		m.tagList.Select(0)
	}
}
