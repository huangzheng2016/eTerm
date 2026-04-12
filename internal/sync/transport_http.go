package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type httpTransport struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewHTTPTransport(baseURL, apiKey string) Transport {
	return &httpTransport{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *httpTransport) Ping() error {
	req, err := http.NewRequest("GET", t.baseURL+"/api/v1/ping", nil)
	if err != nil {
		return err
	}
	t.setAuth(req)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (t *httpTransport) Pull(sinceRev int64) ([]SyncRecord, int64, error) {
	url := fmt.Sprintf("%s/api/v1/records?since=%d", t.baseURL, sinceRev)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	t.setAuth(req)
	resp, err := t.client.Do(req)
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
	req, err := http.NewRequest("POST", t.baseURL+"/api/v1/records", bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	t.setAuth(req)
	resp, err := t.client.Do(req)
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

func (t *httpTransport) setAuth(req *http.Request) {
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
}
