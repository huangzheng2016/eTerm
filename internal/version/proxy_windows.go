//go:build windows

package version

import (
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func updateProxy(req *http.Request) (*url.URL, error) {
	if proxyURL, err := http.ProxyFromEnvironment(req); proxyURL != nil || err != nil {
		return proxyURL, err
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil
	}
	defer k.Close()
	enabled, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil, nil
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return nil, nil
	}
	server = windowsProxyForScheme(server, req.URL.Scheme)
	if server == "" {
		return nil, nil
	}
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	return url.Parse(server)
}

func windowsProxyForScheme(server, scheme string) string {
	if !strings.Contains(server, "=") {
		return strings.TrimSpace(server)
	}
	for _, entry := range strings.Split(server, ";") {
		name, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(strings.TrimSpace(name), scheme) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
