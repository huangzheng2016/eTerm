package sync

import (
	"crypto/tls"
	"net/http"
	"strings"
	"time"
)

func HTTPBaseURLCandidates(raw string) []string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return []string{raw}
	}
	return []string{"https://" + raw, "http://" + raw}
}

func WSURLCandidates(raw, path string) []string {
	bases := HTTPBaseURLCandidates(raw)
	out := make([]string, 0, len(bases))
	for _, base := range bases {
		switch {
		case strings.HasPrefix(base, "https://"):
			out = append(out, "wss://"+strings.TrimPrefix(base, "https://")+path)
		case strings.HasPrefix(base, "http://"):
			out = append(out, "ws://"+strings.TrimPrefix(base, "http://")+path)
		}
	}
	return out
}

// insecureTransport is shared by all InsecureTLS clients so keep-alive
// connections are pooled instead of leaking one idle conn per request.
var insecureTransport = &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}

func HTTPClient(timeout time.Duration, insecureTLS bool) *http.Client {
	client := &http.Client{Timeout: timeout}
	if insecureTLS {
		client.Transport = insecureTransport
	}
	return client
}
