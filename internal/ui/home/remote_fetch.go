package home

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func (m Model) loadRemoteSummary() ([]types.RemotePeer, []types.RemoteHost, error) {
	cfg := esync.LoadConfig(m.db, m.masterKey)
	if !cfg.Enabled || cfg.Passphrase == "" {
		return nil, nil, nil
	}
	serverURL := cfg.ServerURL
	insecureTLS := cfg.InsecureTLS
	if cfg.Mode == "ssh" {
		if cfg.SSHHostID == 0 {
			return nil, nil, nil
		}
		tunnel, err := esync.OpenTunnel(m.db, m.masterKey, cfg.SSHHostID, cfg.RemotePort)
		if err != nil {
			return nil, nil, err
		}
		defer tunnel.Close()
		serverURL = tunnel.BaseURL()
		insecureTLS = false
	}
	if serverURL == "" {
		return nil, nil, nil
	}
	client := esync.HTTPClient(3*time.Second, insecureTLS)
	tenant := cfg.TenantID()
	var peersResp struct {
		Peers []types.RemotePeer `json:"peers"`
	}
	if err := getJSON(client, serverURL, "/api/v1/peers", cfg.APIKey, tenant, &peersResp); err != nil {
		return nil, nil, err
	}
	var hostsResp struct {
		Hosts []types.RemoteHost `json:"hosts"`
	}
	if err := getJSON(client, serverURL, "/api/v1/hosts", cfg.APIKey, tenant, &hostsResp); err != nil {
		return nil, nil, err
	}
	sort.Slice(peersResp.Peers, func(i, j int) bool { return peersResp.Peers[i].Name < peersResp.Peers[j].Name })
	sort.Slice(hostsResp.Hosts, func(i, j int) bool {
		li := remoteHostLabel(hostsResp.Hosts[i])
		lj := remoteHostLabel(hostsResp.Hosts[j])
		if li == lj {
			return hostsResp.Hosts[i].SyncID < hostsResp.Hosts[j].SyncID
		}
		return li < lj
	})
	return peersResp.Peers, hostsResp.Hosts, nil
}

func getJSON(client *http.Client, serverURL, path, apiKey, tenant string, out interface{}) error {
	candidates := esync.HTTPBaseURLCandidates(serverURL)
	if len(candidates) == 0 {
		return fmt.Errorf("no server URL candidates for %q", serverURL)
	}
	var lastErr error
	for _, baseURL := range candidates {
		req, err := http.NewRequest("GET", baseURL+path, nil)
		if err != nil {
			return err
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		if tenant != "" {
			req.Header.Set("X-ETerm-Tenant", tenant)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			io.Copy(io.Discard, resp.Body)
			return fmt.Errorf("GET %s: HTTP %d", baseURL+path, resp.StatusCode)
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
	}
	return lastErr
}

func remoteHostLabel(h types.RemoteHost) string {
	if strings.TrimSpace(h.Alias) != "" {
		return h.Alias
	}
	if strings.TrimSpace(h.Hostname) != "" {
		return h.Hostname
	}
	return h.SyncID
}
