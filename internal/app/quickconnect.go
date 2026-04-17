package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/eterm/eterm/internal/db"
	internalssh "github.com/eterm/eterm/internal/ssh"
	"github.com/eterm/eterm/internal/types"
	"github.com/eterm/eterm/internal/ui"
	"github.com/eterm/eterm/internal/ui/components"
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
	var toastCmd tea.Cmd
	a.toast, toastCmd = a.toast.Show("Quick connecting...", components.ToastInfo, 30*time.Second)
	ptyCols, ptyRows := ptyFromAppSizeForTab(a, SSHTab)

	host := &db.Host{
		Hostname:   msg.Hostname,
		Port:       msg.Port,
		Username:   msg.Username,
		AuthMethod: "agent",
	}

	dial := func() tea.Msg {
		// Fingerprint pre-check
		if internalssh.NeedsFingerprint(database, host.Hostname, host.Port) {
			algo, fp, err := internalssh.ProbeHostKey(host.Hostname, host.Port, 10*time.Second)
			if err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("failed to probe host key: %w", err)}
			}
			return quickConnectFingerprintMsg{
				info: msg,
				confirmInfo: types.FingerprintConfirmMsg{
					Hostname:    host.Hostname,
					Port:        host.Port,
					Algorithm:   algo,
					Fingerprint: fp,
					ConnType:    "quick",
				},
			}
		}

		client, err := internalssh.Connect(internalssh.ConnectConfig{
			Host:      host,
			MasterKey: mk,
			DB:        database,
			FingerprintCallback: func(hostname string, port int, algorithm string, fingerprint string) bool {
				return true
			},
		})
		if err != nil {
			return types.ErrorMsg{Err: fmt.Errorf("quick connect failed: %w", err)}
		}

		is, err := internalssh.NewInteractiveSession(client.Client, ptyRows, ptyCols, false)
		if err != nil {
			client.Client.Close()
			for _, c := range client.Closers {
				_ = c.Close()
			}
			return types.ErrorMsg{Err: fmt.Errorf("failed to start shell: %w", err)}
		}
		is.SetClosers(client.Closers)

		alias := fmt.Sprintf("%s@%s:%d", msg.Username, msg.Hostname, msg.Port)
		return openSSHUITabMsg{is: is, alias: alias, replaceTabAt: -1}
	}
	return a, tea.Batch(toastCmd, reflowWindow(a), dial)
}
