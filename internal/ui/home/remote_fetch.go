package home

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func (m Model) loadRemoteSummary() ([]types.RemotePeer, []types.RemoteHost) {
	cfg := esync.LoadConfig(m.db, m.masterKey)
	if !cfg.Enabled || cfg.Mode != "http" || cfg.ServerURL == "" || cfg.Passphrase == "" {
		return nil, nil
	}
	client := esync.HTTPClient(3*time.Second, cfg.InsecureTLS)
	tenant := cfg.TenantID()
	var peersResp struct {
		Peers []types.RemotePeer `json:"peers"`
	}
	_ = getJSON(client, cfg.ServerURL, "/api/v1/peers", cfg.APIKey, tenant, &peersResp)
	var hostsResp struct {
		Hosts []types.RemoteHost `json:"hosts"`
	}
	_ = getJSON(client, cfg.ServerURL, "/api/v1/hosts", cfg.APIKey, tenant, &hostsResp)
	sort.Slice(peersResp.Peers, func(i, j int) bool { return peersResp.Peers[i].Name < peersResp.Peers[j].Name })
	sort.Slice(hostsResp.Hosts, func(i, j int) bool {
		li := remoteHostLabel(hostsResp.Hosts[i])
		lj := remoteHostLabel(hostsResp.Hosts[j])
		if li == lj {
			return hostsResp.Hosts[i].SyncID < hostsResp.Hosts[j].SyncID
		}
		return li < lj
	})
	return peersResp.Peers, hostsResp.Hosts
}

func getJSON(client *http.Client, serverURL, path, apiKey, tenant string, out interface{}) error {
	var lastErr error
	for _, baseURL := range esync.HTTPBaseURLCandidates(serverURL) {
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
			return nil
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
