package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"

	"gorm.io/gorm"
)

type quickConnectModel struct {
	input textinput.Model
}

type quickConnectFingerprintMsg struct {
	info        types.QuickConnectMsg
	confirmInfo types.FingerprintConfirmMsg
}

func newQuickConnectModel() *quickConnectModel {
	ti := textinput.New()
	ti.Placeholder = "user@host:port"
	ti.CharLimit = 256
	q := &quickConnectModel{input: ti}
	q.syncInputWidth(0)
	return q
}

// syncInputWidth sets textinput width so placeholder is not truncated to one character (bubbles textinput when Width<=0).
func (q *quickConnectModel) syncInputWidth(termW int) {
	iw := 44
	if termW > 0 {
		iw = max(28, termW-20)
		if iw > 78 {
			iw = 78
		}
	}
	q.input.SetWidth(iw)
}

func (q *quickConnectModel) View() string {
	title := ui.TitleStyle.Render("Quick Connect")
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("Format: [user@]host[:port]  |  Enter: connect  |  Esc: cancel  |  click left connect / right cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", q.input.View(), "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
}

func (a App) handleQuickConnectPaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	a.quickConnect.input = inputpaste.TextInput(a.quickConnect.input, msg)
	return a, nil
}

func (a App) handleQuickConnectKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.quickConnect = nil
		return a, nil
	case "enter":
		raw := strings.TrimSpace(a.quickConnect.input.Value())
		if raw == "" {
			a.quickConnect = nil
			return a, nil
		}
		hostname, port, username := parseQuickConnect(raw)
		a.quickConnect = nil
		return a, func() tea.Msg {
			return types.QuickConnectMsg{Hostname: hostname, Port: port, Username: username}
		}
	}
	var cmd tea.Cmd
	a.quickConnect.input, cmd = a.quickConnect.input.Update(msg)
	return a, cmd
}

func parseQuickConnect(raw string) (hostname string, port int, username string) {
	port = 22
	username = "root"

	// user@host:port
	if at := strings.Index(raw, "@"); at >= 0 {
		username = raw[:at]
		raw = raw[at+1:]
	}
	if colon := strings.LastIndex(raw, ":"); colon >= 0 {
		if p, err := strconv.Atoi(raw[colon+1:]); err == nil && p > 0 && p < 65536 {
			port = p
			raw = raw[:colon]
		}
	}
	hostname = raw
	return
}

func (a App) handleQuickConnect(msg types.QuickConnectMsg) (App, tea.Cmd) {
	database := a.db
	mk := a.masterKey
	prefix := "Quick connect"
	var progress func(string)
	var progressCh chan string
	var progressCmd tea.Cmd
	a, progressCh, progressCmd, progress = a.beginConnectProgress(connectStageText(prefix, "verify"))
	ptyCols, ptyRows := ptyFromAppSizeForTab(a, SSHTab)

	host := &db.Host{
		Hostname:   msg.Hostname,
		Port:       msg.Port,
		Username:   msg.Username,
		AuthMethod: "agent",
	}

	dial := func() tea.Msg {
		defer close(progressCh)
		if bm := hostFingerprintDialBlock(database, 0, host.Hostname, host.Port, "quick", 0, 0); bm != nil {
			if fp, ok := bm.(types.FingerprintConfirmMsg); ok {
				return quickConnectFingerprintMsg{info: msg, confirmInfo: fp}
			}
			return bm
		}

		client, err := internalssh.Connect(internalssh.ConnectConfig{
			Host:      host,
			MasterKey: mk,
			DB:        database,
			FingerprintCallback: func(hostname string, port int, algorithm string, fingerprint string) bool {
				return true
			},
			Progress: func(stage internalssh.ConnectStage) {
				progress(connectStageText(prefix, string(stage)))
			},
		})
		if err != nil {
			return types.ConnErrorMsg{Err: err, Target: fmt.Sprintf("%s@%s:%d", msg.Username, msg.Hostname, msg.Port), Retry: msg}
		}

		progress(connectStageText(prefix, "shell"))
		is, err := internalssh.NewInteractiveSession(client.Client, ptyRows, ptyCols, false)
		if err != nil {
			client.Client.Close()
			for _, c := range client.Closers {
				_ = c.Close()
			}
			return types.ConnErrorMsg{Err: err, Target: fmt.Sprintf("%s@%s:%d", msg.Username, msg.Hostname, msg.Port), Retry: msg}
		}
		is.SetClosers(client.Closers)

		alias := fmt.Sprintf("%s@%s:%d", msg.Username, msg.Hostname, msg.Port)
		now := time.Now()
		var savedHost db.Host
		progress(connectStageText(prefix, "save"))
		err = database.Where("hostname = ? AND port = ? AND username = ?", msg.Hostname, msg.Port, msg.Username).First(&savedHost).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			savedHost = *host
			savedHost.Alias = alias
			savedHost.LastConnectedAt = &now
			if err := database.Create(&savedHost).Error; err != nil {
				_ = is.Close()
				return types.ConnErrorMsg{Err: fmt.Errorf("save quick link: %w", err), Target: alias, Retry: msg}
			}
		} else if err == nil {
			if err := database.Model(&db.Host{}).Where("id = ?", savedHost.ID).Update("last_connected_at", now).Error; err != nil {
				_ = is.Close()
				return types.ConnErrorMsg{Err: fmt.Errorf("update quick link: %w", err), Target: alias, Retry: msg}
			}
		} else {
			_ = is.Close()
			return types.ConnErrorMsg{Err: fmt.Errorf("load quick link: %w", err), Target: alias, Retry: msg}
		}
		history := db.ConnectionHistory{HostID: savedHost.ID, ConnectedAt: now, Status: "success"}
		if err := database.Create(&history).Error; err != nil {
			_ = is.Close()
			return types.ConnErrorMsg{Err: fmt.Errorf("save quick link history: %w", err), Target: alias, Retry: msg}
		}
		return openSSHUITabMsg{is: is, alias: alias, hostID: savedHost.ID, historyID: history.ID, replaceTabAt: -1}
	}
	return a, tea.Batch(progressCmd, dial)
}
