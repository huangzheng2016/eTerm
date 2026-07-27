//go:build !windows

package version

import (
	"net/http"
	"net/url"
)

func updateProxy(req *http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment(req)
}
