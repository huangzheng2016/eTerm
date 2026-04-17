package batchresultview

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/security"
	internalssh "github.com/eterm/eterm/internal/ssh"
	"github.com/eterm/eterm/internal/types"
)

var jobIDGen atomic.Uint64

type hostState struct {
	HostID uint
	Label  string
	Status string
	Output strings.Builder
}

type HostStartMsg struct {
	JobID  uint64
	HostID uint
}

type HostOutputMsg struct {
	JobID  uint64
	HostID uint
	Data   string
}

type HostDoneMsg struct {
	JobID  uint64
	HostID uint
	Err    error
}

type AllDoneMsg struct {
	JobID uint64
}

type Model struct {
	jobID     uint64
	db        *gorm.DB
	masterKey *security.MasterKeyManager
	command   string
	hostIDs   []uint
	hosts     []hostState
	cursor    int
	scroll    int
	width     int
	height    int
	started   bool
	done      bool
	ch        chan tea.Msg
	running   int
	success   int
	failed    int
}

func New(database *gorm.DB, masterKey *security.MasterKeyManager, hostIDs []uint, command string) *Model {
	m := &Model{
		jobID:     jobIDGen.Add(1),
		db:        database,
		masterKey: masterKey,
		command:   command,
		hostIDs:   append([]uint(nil), hostIDs...),
		ch:        make(chan tea.Msg, 2048),
	}
	m.loadHostLabels()
	return m
}

func (m *Model) JobID() uint64 { return m.jobID }

func (m *Model) loadHostLabels() {
	m.hosts = make([]hostState, 0, len(m.hostIDs))
	for _, id := range m.hostIDs {
		label := fmt.Sprintf("#%d", id)
		var host db.Host
		if err := m.db.First(&host, id).Error; err == nil {
			label = host.Alias
			if strings.TrimSpace(label) == "" {
				label = fmt.Sprintf("%s@%s", host.Username, host.Hostname)
			}
		}
		m.hosts = append(m.hosts, hostState{HostID: id, Label: label, Status: "queued"})
	}
}

func (m *Model) Init() tea.Cmd {
	if !m.started {
		m.started = true
		go m.run()
	}
	return waitEvent(m)
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func waitEvent(m *Model) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) emit(msg tea.Msg) {
	select {
	case m.ch <- msg:
	default:
	}
}

func (m *Model) run() {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, id := range m.hostIDs {
		wg.Add(1)
		go func(hostID uint) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			m.runHost(hostID)
		}(id)
	}
	wg.Wait()
	m.emit(AllDoneMsg{JobID: m.jobID})
	close(m.ch)
}

func (m *Model) runHost(hostID uint) {
	m.emit(HostStartMsg{JobID: m.jobID, HostID: hostID})

	var host db.Host
	if err := m.db.Preload("Key").First(&host, hostID).Error; err != nil {
		m.emit(HostOutputMsg{JobID: m.jobID, HostID: hostID, Data: err.Error() + "\n"})
		m.emit(HostDoneMsg{JobID: m.jobID, HostID: hostID, Err: err})
		return
	}
	if internalssh.NeedsFingerprint(m.db, host.Hostname, host.Port) {
		err := fmt.Errorf("host key not trusted yet")
		m.emit(HostOutputMsg{JobID: m.jobID, HostID: hostID, Data: err.Error() + "\n"})
		m.emit(HostDoneMsg{JobID: m.jobID, HostID: hostID, Err: err})
		return
	}

	var jumpHost *db.Host
	var jumpKey *db.SSHKey
	if host.JumpHostID != nil {
		var jh db.Host
		if err := m.db.Preload("Key").First(&jh, *host.JumpHostID).Error; err == nil {
			jumpHost = &jh
			if jh.KeyID != nil {
				jumpKey = &jh.Key
			}
		}
	}

	var hostKey *db.SSHKey
	if host.KeyID != nil {
		hostKey = &host.Key
	}

	conn, err := internalssh.Connect(internalssh.ConnectConfig{
		Host:      &host,
		Key:       hostKey,
		JumpHost:  jumpHost,
		JumpKey:   jumpKey,
		MasterKey: m.masterKey,
		DB:        m.db,
		FingerprintCallback: func(hostname string, port int, algorithm string, fingerprint string) bool {
			return false
		},
	})
	if err != nil {
		m.emit(HostOutputMsg{JobID: m.jobID, HostID: hostID, Data: err.Error() + "\n"})
		m.emit(HostDoneMsg{JobID: m.jobID, HostID: hostID, Err: err})
		return
	}
	defer conn.Close()

	sess, err := conn.Client.NewSession()
	if err != nil {
		m.emit(HostOutputMsg{JobID: m.jobID, HostID: hostID, Data: err.Error() + "\n"})
		m.emit(HostDoneMsg{JobID: m.jobID, HostID: hostID, Err: err})
		return
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		m.emit(HostOutputMsg{JobID: m.jobID, HostID: hostID, Data: err.Error() + "\n"})
		m.emit(HostDoneMsg{JobID: m.jobID, HostID: hostID, Err: err})
		return
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		m.emit(HostOutputMsg{JobID: m.jobID, HostID: hostID, Data: err.Error() + "\n"})
		m.emit(HostDoneMsg{JobID: m.jobID, HostID: hostID, Err: err})
		return
	}

	var readWG sync.WaitGroup
	readWG.Add(2)
	go m.readPipe(hostID, stdout, &readWG)
	go m.readPipe(hostID, stderr, &readWG)

	err = sess.Start("sh -lc " + shellQuote(m.command))
	if err == nil {
		err = sess.Wait()
	}
	readWG.Wait()
	m.emit(HostDoneMsg{JobID: m.jobID, HostID: hostID, Err: err})
}

func (m *Model) readPipe(hostID uint, r io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			m.emit(HostOutputMsg{JobID: m.jobID, HostID: hostID, Data: string(buf[:n])})
		}
		if err != nil {
			return
		}
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func (m *Model) selectedOutput() string {
	if m.cursor < 0 || m.cursor >= len(m.hosts) {
		return ""
	}
	return m.hosts[m.cursor].Output.String()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case nil:
		m.done = true
		m.running = 0
		return m, nil
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case HostStartMsg:
		if msg.JobID != m.jobID {
			return m, nil
		}
		if i := m.indexForHost(msg.HostID); i >= 0 {
			m.hosts[i].Status = "running"
			m.running++
		}
		return m, waitEvent(m)
	case HostOutputMsg:
		if msg.JobID != m.jobID {
			return m, nil
		}
		if i := m.indexForHost(msg.HostID); i >= 0 {
			m.hosts[i].Output.WriteString(msg.Data)
		}
		return m, waitEvent(m)
	case HostDoneMsg:
		if msg.JobID != m.jobID {
			return m, nil
		}
		if i := m.indexForHost(msg.HostID); i >= 0 {
			if m.running > 0 {
				m.running--
			}
			if msg.Err != nil {
				m.hosts[i].Status = "failed"
				m.failed++
				if m.hosts[i].Output.Len() == 0 {
					m.hosts[i].Output.WriteString(msg.Err.Error())
					m.hosts[i].Output.WriteByte('\n')
				}
			} else {
				m.hosts[i].Status = "ok"
				m.success++
			}
		}
		return m, waitEvent(m)
	case AllDoneMsg:
		if msg.JobID != m.jobID {
			return m, nil
		}
		m.done = true
		return m, waitEvent(m)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.scroll = 0
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.hosts)-1 {
				m.cursor++
				m.scroll = 0
			}
			return m, nil
		case "pgup":
			m.scroll -= 10
			if m.scroll < 0 {
				m.scroll = 0
			}
			return m, nil
		case "pgdown":
			m.scroll += 10
			return m, nil
		}
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || len(m.hosts) == 0 {
			return m, nil
		}
		listW := m.listWidth()
		if msg.X < 0 || msg.X >= listW {
			return m, nil
		}
		row := msg.Y - 5
		if row < 0 || row >= len(m.hosts) {
			return m, nil
		}
		m.cursor = row
		m.scroll = 0
		return m, nil
	case tea.MouseWheelMsg:
		if len(m.hosts) == 0 {
			return m, nil
		}
		listW := m.listWidth()
		switch msg.Button {
		case tea.MouseWheelUp, tea.MouseWheelLeft:
			if msg.X >= 0 && msg.X < listW {
				if m.cursor > 0 {
					m.cursor--
					m.scroll = 0
				}
				return m, nil
			}
			m.scroll -= 3
			if m.scroll < 0 {
				m.scroll = 0
			}
			return m, nil
		case tea.MouseWheelDown, tea.MouseWheelRight:
			if msg.X >= 0 && msg.X < listW {
				if m.cursor < len(m.hosts)-1 {
					m.cursor++
					m.scroll = 0
				}
				return m, nil
			}
			m.scroll += 3
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) listWidth() int {
	listW := m.width / 3
	if listW < 22 {
		listW = 22
	}
	if listW > m.width-24 {
		listW = m.width / 2
	}
	bodyW := m.width - listW - 2
	if bodyW < 20 {
		listW = m.width
	}
	if listW < 1 {
		listW = 1
	}
	return listW
}

func (m *Model) indexForHost(hostID uint) int {
	for i := range m.hosts {
		if m.hosts[i].HostID == hostID {
			return i
		}
	}
	return -1
}
