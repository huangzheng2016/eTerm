package home

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/keymatch"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
	"gorm.io/gorm"
)

type hostItem struct {
	host db.Host
}

type gridEntry struct {
	peer *types.RemotePeer
	host *db.Host
}

func displayGroupName(g string) string {
	if strings.TrimSpace(g) == "" {
		return "Default"
	}
	return g
}

func groupPrefix(g string) string {
	name := displayGroupName(g)
	if strings.EqualFold(strings.TrimSpace(name), "Default") {
		return ""
	}
	return "[" + name + "] "
}

func (i hostItem) FilterValue() string {
	prefix := groupPrefix(i.host.Group)
	name := i.host.Alias
	if name == "" {
		name = fmt.Sprintf("%s@%s", i.host.Username, i.host.Hostname)
	}
	return prefix + name + " " + i.host.Hostname + " " + i.host.Tags
}

func (i hostItem) Title() string {
	prefix := groupPrefix(i.host.Group)
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
	count int
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
	lastClickIdx int

	gridCursor int
	gridLayout gridLayout

	remoteRefreshArmed bool

	mode        viewMode
	allHosts    []db.Host
	allTags     []string
	selectedTag string
	tagList     list.Model

	showHidden bool

	hostStatus              map[uint]HostStatus
	lastConnectivityProbeAt time.Time
	connectivityProbeSeq    uint64

	remotePeers []types.RemotePeer
	remoteHosts []types.RemoteHost

	kmCfg keymatch.Config

	helpKeys           []string
	quickConnectKeys   []string
	showHiddenKeys     []string
	hideHostKeys       []string
	sessionHistoryKeys []string
	toggleSelectKeys   []string
	batchTagKeys       []string
	batchActionKeys    []string
	localTerminalKeys  []string

	selectedHosts map[uint]struct{}

	gridStatusWords bool
}

type HomeKeyConfig struct {
	KmCfg          keymatch.Config
	Keys           listKeyMap
	Help           []string
	QuickConnect   []string
	ShowHidden     []string
	HideHost       []string
	SessionHistory []string
	ToggleSelect   []string
	BatchTag       []string
	BatchActions   []string
	Tmux           []string
	LocalTerminal  []string
}

func New(database *gorm.DB, masterKey *security.MasterKeyManager, hkc HomeKeyConfig) Model {
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
	l.KeyMap.Filter = hkc.Keys.Search
	return Model{
		list:               l,
		keys:               hkc.Keys,
		db:                 database,
		masterKey:          masterKey,
		lastClickIdx:       -1,
		tagList:            newTagList(),
		kmCfg:              hkc.KmCfg,
		helpKeys:           hkc.Help,
		quickConnectKeys:   hkc.QuickConnect,
		showHiddenKeys:     hkc.ShowHidden,
		hideHostKeys:       hkc.HideHost,
		sessionHistoryKeys: hkc.SessionHistory,
		toggleSelectKeys:   hkc.ToggleSelect,
		batchTagKeys:       hkc.BatchTag,
		batchActionKeys:    hkc.BatchActions,
		localTerminalKeys:  hkc.LocalTerminal,
		selectedHosts:      make(map[uint]struct{}),
	}
}

func (m Model) WithUpdatedKeys(hkc HomeKeyConfig) Model {
	m.keys = hkc.Keys
	m.list.KeyMap.Filter = hkc.Keys.Search
	m.kmCfg = hkc.KmCfg
	m.helpKeys = hkc.Help
	m.quickConnectKeys = hkc.QuickConnect
	m.showHiddenKeys = hkc.ShowHidden
	m.hideHostKeys = hkc.HideHost
	m.sessionHistoryKeys = hkc.SessionHistory
	m.toggleSelectKeys = hkc.ToggleSelect
	m.batchTagKeys = hkc.BatchTag
	m.batchActionKeys = hkc.BatchActions
	m.localTerminalKeys = hkc.LocalTerminal
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

func (m Model) gridEntries() []gridEntry {
	hosts := m.gridHosts()
	entries := make([]gridEntry, 0, len(m.remotePeers)+len(hosts))
	if m.mode == groupView && m.list.FilterState() == list.Unfiltered {
		for i := range m.remotePeers {
			peer := m.remotePeers[i]
			entries = append(entries, gridEntry{peer: &peer})
		}
	}
	for i := range hosts {
		host := hosts[i]
		entries = append(entries, gridEntry{host: &host})
	}
	return entries
}

func (m Model) SelectedHost() *db.Host {
	entries := m.gridEntries()
	if len(entries) == 0 {
		return nil
	}
	idx := m.gridCursor
	if idx < 0 || idx >= len(entries) {
		idx = 0
	}
	return entries[idx].host
}

func (m Model) SelectedPeer() *types.RemotePeer {
	entries := m.gridEntries()
	if len(entries) == 0 {
		return nil
	}
	idx := m.gridCursor
	if idx < 0 || idx >= len(entries) {
		idx = 0
	}
	return entries[idx].peer
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

func (m Model) batchActionHostIDs() []uint {
	if len(m.selectedHosts) > 0 {
		return m.batchHostIDs()
	}
	if m.mode == tagView && m.selectedTag != "" {
		hosts := m.gridHosts()
		out := make([]uint, 0, len(hosts))
		for _, h := range hosts {
			out = append(out, h.ID)
		}
		return out
	}
	h := m.SelectedHost()
	if h == nil {
		return nil
	}
	group := strings.TrimSpace(h.Group)
	if group == "" {
		return []uint{h.ID}
	}
	all := m.filterHidden(m.allHosts)
	var out []uint
	for _, cand := range all {
		if strings.TrimSpace(cand.Group) == group {
			out = append(out, cand.ID)
		}
	}
	if len(out) == 0 {
		return []uint{h.ID}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (m Model) loadHosts() tea.Cmd {
	return func() tea.Msg {
		var hosts []db.Host
		err := m.db.Order("last_connected_at DESC NULLS LAST, alias ASC").Find(&hosts).Error
		return hostsLoadedMsg{hosts: hosts, err: err}
	}
}

func (m Model) loadRemote(silent bool) tea.Cmd {
	return func() tea.Msg {
		peers, hosts, err := m.loadRemoteSummary()
		return types.RemoteDaemonLoadedMsg{Peers: peers, Hosts: hosts, Err: err, Silent: silent}
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
				continue
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
