package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/fwdview"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

// hostForwardState shares one SSH connection for multiple port-forward rules on the same host.
type hostForwardState struct {
	res   *internalssh.ConnectResult
	rules map[uint]*internalssh.PortForwardCloser
}

// forwardRuleAttachMsg completes registration of a started listener after dial or session reuse.
type forwardRuleAttachMsg struct {
	ruleID uint
	hostID uint
	res    *internalssh.ConnectResult // nil when reusing an existing hostForwardState
	pfc    *internalssh.PortForwardCloser
}

func (a App) ruleForwardRunning(ruleID uint) bool {
	for _, st := range a.forwardByHost {
		if st == nil || st.rules == nil {
			continue
		}
		if _, ok := st.rules[ruleID]; ok {
			return true
		}
	}
	return false
}

func startForwardOne(client *ssh.Client, rule db.PortForward) (*internalssh.PortForwardCloser, error) {
	switch rule.Direction {
	case "local":
		return internalssh.StartLocalForward(client, rule.LocalPort, rule.RemoteHost, rule.RemotePort)
	case "remote":
		return internalssh.StartRemoteForward(client, rule.RemotePort, rule.RemoteHost, rule.LocalPort)
	case "dynamic":
		return internalssh.StartDynamicForward(client, rule.LocalPort)
	default:
		return nil, fmt.Errorf("unknown forward type %q", rule.Direction)
	}
}

func forwardDial(database *gorm.DB, mk *security.MasterKeyManager, ruleID uint) tea.Msg {
	var rule db.PortForward
	if err := database.Preload("Host").Preload("Host.Key").Preload("Host.JumpHost").Preload("Host.JumpHost.Key").
		First(&rule, ruleID).Error; err != nil {
		return types.ForwardRuleResultMsg{RuleID: ruleID, Err: err}
	}
	host := rule.Host

	if bm := hostFingerprintDialBlock(database, host.ID, host.Hostname, host.Port, "forward", 0, ruleID); bm != nil {
		switch m := bm.(type) {
		case types.FingerprintConfirmMsg:
			return m
		case types.ErrorMsg:
			return types.ForwardRuleResultMsg{RuleID: ruleID, Err: m.Err}
		}
		return types.ForwardRuleResultMsg{RuleID: ruleID, Err: fmt.Errorf("fingerprint check failed")}
	}

	var jumpHost *db.Host
	var jumpKey *db.SSHKey
	if host.JumpHostID != nil && host.JumpHost != nil {
		jumpHost = host.JumpHost
		if host.JumpHost.KeyID != nil {
			jumpKey = &host.JumpHost.Key
		}
	}

	var hostKey *db.SSHKey
	if host.KeyID != nil {
		hostKey = &host.Key
	}

	res, err := internalssh.Connect(internalssh.ConnectConfig{
		Host:      &host,
		Key:       hostKey,
		JumpHost:  jumpHost,
		JumpKey:   jumpKey,
		MasterKey: mk,
		DB:        database,
		FingerprintCallback: func(hostname string, port int, algorithm string, fingerprint string) bool {
			return true
		},
	})
	if err != nil {
		return types.ForwardRuleResultMsg{RuleID: ruleID, Err: fmt.Errorf("SSH connection failed: %w", err)}
	}

	pfc, err := startForwardOne(res.Client, rule)
	if err != nil {
		res.Close()
		return types.ForwardRuleResultMsg{RuleID: ruleID, Err: err}
	}

	return forwardRuleAttachMsg{
		ruleID: ruleID,
		hostID: host.ID,
		res:    res,
		pfc:    pfc,
	}
}

func (a App) handleForwardRuleStart(ruleID uint) (App, tea.Cmd) {
	if a.ruleForwardRunning(ruleID) {
		return a, nil
	}

	var rule db.PortForward
	if err := a.db.Preload("Host").First(&rule, ruleID).Error; err != nil {
		return a, func() tea.Msg {
			return types.ForwardRuleResultMsg{RuleID: ruleID, Err: err}
		}
	}
	hostID := rule.HostID

	if a.forwardByHost != nil {
		if st, ok := a.forwardByHost[hostID]; ok && st != nil && st.res != nil {
			// Capture client by value — st may be mutated or closed by a concurrent
			// handleForwardRuleStop before this closure executes.
			client := st.res.Client
			r := rule
			return a, func() tea.Msg {
				pfc, err := startForwardOne(client, r)
				if err != nil {
					return types.ForwardRuleResultMsg{RuleID: ruleID, Err: err}
				}
				return forwardRuleAttachMsg{ruleID: ruleID, hostID: hostID, res: nil, pfc: pfc}
			}
		}
	}

	database := a.db
	mk := a.masterKey
	return a, func() tea.Msg { return forwardDial(database, mk, ruleID) }
}

func (a App) attachForward(msg forwardRuleAttachMsg) (App, tea.Cmd) {
	if a.forwardByHost == nil {
		a.forwardByHost = make(map[uint]*hostForwardState)
	}
	st := a.forwardByHost[msg.hostID]
	if msg.res != nil {
		if old := a.forwardByHost[msg.hostID]; old != nil && old.res != nil {
			// A concurrent dial already attached this host; the displaced
			// session and its listeners must not leak.
			for _, pfc := range old.rules {
				if pfc != nil {
					_ = pfc.Close()
				}
			}
			old.res.Close()
		}
		st = &hostForwardState{res: msg.res, rules: make(map[uint]*internalssh.PortForwardCloser)}
		a.forwardByHost[msg.hostID] = st
	}
	if st == nil {
		return a.broadcastForwardResult(types.ForwardRuleResultMsg{
			RuleID: msg.ruleID, Err: fmt.Errorf("internal: missing SSH session"), Running: false,
		})
	}
	st.rules[msg.ruleID] = msg.pfc
	return a.broadcastForwardResult(types.ForwardRuleResultMsg{RuleID: msg.ruleID, Running: true})
}

func (a App) handleForwardRuleStop(ruleID uint) (App, tea.Cmd) {
	if a.forwardByHost == nil {
		return a, nil
	}
	for hid, st := range a.forwardByHost {
		if st == nil || st.rules == nil {
			continue
		}
		pfc, ok := st.rules[ruleID]
		if !ok {
			continue
		}
		_ = pfc.Close()
		delete(st.rules, ruleID)
		if len(st.rules) == 0 {
			if st.res != nil {
				st.res.Close()
			}
			delete(a.forwardByHost, hid)
		}
		return a.broadcastForwardResult(types.ForwardRuleResultMsg{RuleID: ruleID, Running: false})
	}
	return a, nil
}

func (a App) closeAllForwardSessions() App {
	if a.forwardByHost == nil {
		return a
	}
	for _, st := range a.forwardByHost {
		if st == nil {
			continue
		}
		for _, pfc := range st.rules {
			if pfc != nil {
				_ = pfc.Close()
			}
		}
		if st.res != nil {
			st.res.Close()
		}
	}
	a.forwardByHost = nil
	return a
}

func (a App) broadcastForwardResult(msg types.ForwardRuleResultMsg) (App, tea.Cmd) {
	var cmds []tea.Cmd
	for i := range a.tabs {
		fm, ok := a.tabs[i].Model.(fwdview.Model)
		if !ok {
			continue
		}
		u, c := fm.Update(msg)
		a.tabs[i].Model = u
		if c != nil {
			cmds = append(cmds, c)
		}
	}
	return a, tea.Batch(cmds...)
}
