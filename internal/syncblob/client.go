package syncblob

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/huangzheng2016/eTerm/internal/clipboardimg"
)

type Progress struct {
	TotalBytes int64
	SentBytes  int64
}

type ProgressCallback func(Progress)

type Client struct {
	BaseURL  string
	BaseURLs []string
	APIKey   string
	Tenant   string
	HTTP     *http.Client
}

type UploadResult struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Mime      string    `json:"mime"`
	Bytes     int64     `json:"bytes"`
	ExpiresAt time.Time `json:"expires_at"`
	BaseURL   string    `json:"-"`
}

func (c *Client) Upload(img *clipboardimg.Image, progress ProgressCallback) (*UploadResult, error) {
	if img == nil {
		return nil, fmt.Errorf("image is nil")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	var lastErr error
	for _, baseURL := range c.baseURLs() {
		out, err := c.uploadTo(client, baseURL, img, progress)
		if err == nil {
			return out, nil
		}
		if _, ok := err.(httpStatusError); ok {
			return nil, err
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("server URL is required")
}

func (c *Client) baseURLs() []string {
	if len(c.BaseURLs) > 0 {
		out := make([]string, 0, len(c.BaseURLs))
		for _, baseURL := range c.BaseURLs {
			baseURL = strings.TrimRight(baseURL, "/")
			if baseURL != "" {
				out = append(out, baseURL)
			}
		}
		return out
	}
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		return nil
	}
	return []string{baseURL}
}

func (c *Client) uploadTo(client *http.Client, baseURL string, img *clipboardimg.Image, progress ProgressCallback) (*UploadResult, error) {
	reader := &progressReader{
		data:     img.Data,
		progress: progress,
		total:    int64(len(img.Data)),
	}
	req, err := http.NewRequest("POST", baseURL+"/api/v1/blobs", reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if c.Tenant != "" {
		req.Header.Set("X-ETerm-Tenant", c.Tenant)
	}
	req.Header.Set("X-ETerm-Blob-Mime", img.Mime)
	req.Header.Set("X-ETerm-Blob-Filename", img.Filename)
	req.ContentLength = int64(len(img.Data))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, httpStatusError{StatusCode: resp.StatusCode, Body: string(bytes.TrimSpace(body))}
	}
	var out UploadResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	out.BaseURL = baseURL
	return &out, nil
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

type progressReader struct {
	data     []byte
	sent     int64
	total    int64
	progress ProgressCallback
}

func (r *progressReader) Read(p []byte) (int, error) {
	if r.sent >= r.total {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.sent:])
	r.sent += int64(n)
	if r.progress != nil {
		r.progress(Progress{TotalBytes: r.total, SentBytes: r.sent})
	}
	return n, nil
}
