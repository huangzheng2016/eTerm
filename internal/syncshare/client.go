package syncshare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/huangzheng2016/eTerm/internal/sync"
)

type shareResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type httpStatusError struct {
	StatusCode int
	Body       string
}

func (e httpStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

func CreateShare(ctx context.Context, cfg sync.Config, peerID, name string, maxHours int, target, sessionID string) (string, time.Time, error) {
	body, err := json.Marshal(struct {
		PeerID    string `json:"peer_id"`
		Name      string `json:"name"`
		MaxHours  int    `json:"max_hours"`
		Target    string `json:"target,omitempty"`
		SessionID string `json:"session_id,omitempty"`
	}{PeerID: peerID, Name: name, MaxHours: maxHours, Target: target, SessionID: sessionID})
	if err != nil {
		return "", time.Time{}, err
	}
	var shareURL string
	var expiresAt time.Time
	err = forEachBase(cfg, func(base string) error {
		var out shareResponse
		if err := doRequest(ctx, cfg, base, http.MethodPost, "/api/v1/shares", bytes.NewReader(body), &out); err != nil {
			return err
		}
		shareURL = out.URL
		if strings.HasPrefix(shareURL, "/") {
			shareURL = base + shareURL
		}
		expiresAt = out.ExpiresAt
		return nil
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return shareURL, expiresAt, nil
}

func forEachBase(cfg sync.Config, fn func(base string) error) error {
	var lastErr error
	for _, base := range sync.HTTPBaseURLCandidates(cfg.ServerURL) {
		err := fn(base)
		if err == nil {
			return nil
		}
		var statusErr httpStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode < 500 {
			return err
		}
		lastErr = err
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("server URL is required")
}

func doRequest(ctx context.Context, cfg sync.Config, baseURL, method, path string, body io.Reader, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	if tenant := cfg.TenantID(); tenant != "" {
		req.Header.Set("X-ETerm-Tenant", tenant)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := sync.HTTPClient(30*time.Second, cfg.InsecureTLS).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return httpStatusError{StatusCode: resp.StatusCode, Body: string(bytes.TrimSpace(b))}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
