package sshconfig

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParsedHost represents a single Host block from ~/.ssh/config.
type ParsedHost struct {
	Alias        string
	Hostname     string
	Port         int
	Username     string
	IdentFile    string
	ProxyJump    string
	ProxyCommand string
}

// ParseSSHConfig reads and parses an SSH config file.
func ParseSSHConfig(path string) ([]ParsedHost, error) {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hosts []ParsedHost
	var current *ParsedHost

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			parts = strings.SplitN(line, "=", 2)
		}
		if len(parts) < 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if strings.EqualFold(key, "Host") {
			// Skip wildcard entries
			if strings.Contains(val, "*") || strings.Contains(val, "?") {
				current = nil
				continue
			}
			h := ParsedHost{Alias: val, Port: 22}
			hosts = append(hosts, h)
			current = &hosts[len(hosts)-1]
			continue
		}

		if current == nil {
			continue
		}

		switch strings.ToLower(key) {
		case "hostname":
			current.Hostname = val
		case "port":
			if p, err := strconv.Atoi(val); err == nil {
				current.Port = p
			}
		case "user":
			current.Username = val
		case "identityfile":
			current.IdentFile = expandPath(val)
		case "proxyjump":
			current.ProxyJump = val
		case "proxycommand":
			current.ProxyCommand = val
		}
	}

	// Fill in defaults: if no HostName, use Alias
	for i := range hosts {
		if hosts[i].Hostname == "" {
			hosts[i].Hostname = hosts[i].Alias
		}
	}

	return hosts, scanner.Err()
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
