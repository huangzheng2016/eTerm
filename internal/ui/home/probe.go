package home

import (
	"fmt"
	"net"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
)

// HostStatus represents the online status of a host.
type HostStatus int

const (
	StatusUnknown HostStatus = iota
	StatusOnline
	StatusOffline
)

const (
	probeTimeout        = 3 * time.Second
	maxProbeConcurrency = 20
)

type probeResult struct {
	hostID uint
	status HostStatus
}

// probeResultMsg carries a single probe result plus the channel to continue reading.
type probeResultMsg struct {
	probeResult
	ch <-chan probeResult
}

// probeHosts returns a tea.Cmd that streams probe results one at a time.
func probeHosts(hosts []db.Host) tea.Cmd {
	if len(hosts) == 0 {
		return nil
	}

	ch := make(chan probeResult, len(hosts))
	sem := make(chan struct{}, maxProbeConcurrency)
	var wg sync.WaitGroup

	for _, h := range hosts {
		if h.Hostname == "" {
			continue
		}
		wg.Add(1)
		go func(id uint, addr string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			conn, err := net.DialTimeout("tcp", addr, probeTimeout)
			if err != nil {
				ch <- probeResult{hostID: id, status: StatusOffline}
				return
			}
			_ = conn.Close()
			ch <- probeResult{hostID: id, status: StatusOnline}
		}(h.ID, fmt.Sprintf("%s:%d", h.Hostname, h.Port))
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return readProbe(ch)
}

func readProbe(ch <-chan probeResult) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return nil
		}
		return probeResultMsg{probeResult: r, ch: ch}
	}
}
