package version

import (
	"net/http"
	"time"
)

func updateHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = updateProxy
	return &http.Client{Transport: transport, Timeout: timeout}
}
