package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type httpTransport struct {
	baseURLs []string
	apiKey   string
	tenant   string
	client   *http.Client
	mu       sync.Mutex
	active   string
}

func NewHTTPTransport(baseURL, apiKey string) Transport {
	return NewHTTPTransportWithOptions(baseURL, apiKey, "", false)
}

func NewHTTPTransportWithTenant(baseURL, apiKey, tenant string) Transport {
	return NewHTTPTransportWithOptions(baseURL, apiKey, tenant, false)
}

func NewHTTPTransportWithOptions(baseURL, apiKey, tenant string, insecureTLS bool) Transport {
	return &httpTransport{
		baseURLs: HTTPBaseURLCandidates(baseURL),
		apiKey:   apiKey,
		tenant:   tenant,
		client:   HTTPClient(30*time.Second, insecureTLS),
	}
}

func (t *httpTransport) Ping() error {
	resp, err := t.do("GET", "/api/v1/ping", nil, "")
	if resp != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	return err
}

func (t *httpTransport) Pull(sinceRev int64) ([]SyncRecord, int64, error) {
	resp, err := t.do("GET", fmt.Sprintf("/api/v1/records?since=%d", sinceRev), nil, "")
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result struct {
		Records  []SyncRecord `json:"records"`
		Revision int64        `json:"revision"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MB
	if err != nil {
		return nil, 0, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, err
	}
	return result.Records, result.Revision, nil
}

func (t *httpTransport) Push(records []SyncRecord) (int64, error) {
	body := struct {
		Records []SyncRecord `json:"records"`
	}{Records: records}
	data, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	resp, err := t.do("POST", "/api/v1/records", data, "application/json")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result struct {
		Revision int64 `json:"revision"`
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}
	return result.Revision, nil
}

func (t *httpTransport) Close() error { return nil }

func (t *httpTransport) do(method, path string, body []byte, contentType string) (*http.Response, error) {
	candidates := t.candidates()
	var lastErr error
	for _, baseURL := range candidates {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, baseURL+path, reader)
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		t.setAuth(req)
		resp, err := t.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == 200 {
			t.setActive(baseURL)
			return resp, nil
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("server URL is required")
}

func (t *httpTransport) candidates() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active != "" {
		return append([]string{t.active}, removeURL(t.baseURLs, t.active)...)
	}
	return append([]string(nil), t.baseURLs...)
}

func (t *httpTransport) setActive(baseURL string) {
	t.mu.Lock()
	t.active = baseURL
	t.mu.Unlock()
}

func removeURL(urls []string, skip string) []string {
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		if u != skip {
			out = append(out, u)
		}
	}
	return out
}

func (t *httpTransport) setAuth(req *http.Request) {
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	if t.tenant != "" {
		req.Header.Set("X-ETerm-Tenant", t.tenant)
	}
}
