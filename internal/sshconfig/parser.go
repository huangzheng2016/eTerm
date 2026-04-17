package sshconfig

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ParsedHost struct {
	Alias                    string
	Hostname                 string
	Port                     int
	Username                 string
	IdentFile                string
	KeyName                  string
	ProxyJump                string
	ProxyCommand             string
	ProxyType                string
	ProxyHost                string
	ProxyPort                int
	ProxyUser                string
	GSSAPIAuthentication     bool
	GSSAPISource             string
	GSSAPIKeytab             string
	KrbPrincipal             string
	ForwardAgent             bool
	RemoteCommand            string
	PreferredAuthentications []string
	ExtraSSHOptions          string
	Group                    string
	Tags                     string
	Description              string
	AuthMethod               string
}

type etermMetadata struct {
	Group           string `json:"group,omitempty"`
	Tags            string `json:"tags,omitempty"`
	Description     string `json:"description,omitempty"`
	AuthMethod      string `json:"auth_method,omitempty"`
	KeyName         string `json:"key_name,omitempty"`
	ProxyType       string `json:"proxy_type,omitempty"`
	ProxyHost       string `json:"proxy_host,omitempty"`
	ProxyPort       int    `json:"proxy_port,omitempty"`
	ProxyUser       string `json:"proxy_user,omitempty"`
	GSSAPISource    string `json:"gssapi_source,omitempty"`
	GSSAPIKeytab    string `json:"gssapi_keytab,omitempty"`
	KrbPrincipal    string `json:"krb_principal,omitempty"`
}

func ParseSSHConfig(path string) ([]ParsedHost, error) {
	path = expandPath(path)
	visited := map[string]bool{}
	var hosts []ParsedHost
	if err := parseSSHConfigFile(path, visited, &hosts); err != nil {
		return nil, err
	}
	for i := range hosts {
		if hosts[i].Hostname == "" {
			hosts[i].Hostname = hosts[i].Alias
		}
		if hosts[i].Port == 0 {
			hosts[i].Port = 22
		}
	}
	return hosts, nil
}

func parseSSHConfigFile(path string, visited map[string]bool, hosts *[]ParsedHost) error {
	path = expandPath(path)
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if visited[abs] {
		return nil
	}
	visited[abs] = true

	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer f.Close()

	var current *ParsedHost
	var extraLines []string
	var pendingMeta *etermMetadata
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		rawLine := scanner.Text()
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			if current == nil {
				continue
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if current != nil {
				extraLines = append(extraLines, trimmed)
				continue
			}
			if meta, ok := parseMetadataComment(trimmed); ok {
				pendingMeta = &meta
			}
			continue
		}

		key, val, ok := splitDirective(trimmed)
		if !ok {
			if current != nil {
				extraLines = append(extraLines, trimmed)
			} else {
				pendingMeta = nil
			}
			continue
		}

		lkey := strings.ToLower(key)
		switch lkey {
		case "include":
			if current != nil {
				extraLines = append(extraLines, trimmed)
				continue
			}
			for _, match := range resolveIncludePaths(filepath.Dir(abs), val) {
				if err := parseSSHConfigFile(match, visited, hosts); err != nil {
					return err
				}
			}
			pendingMeta = nil
			continue
		case "match":
			if current != nil {
				current.ExtraSSHOptions = strings.Join(extraLines, "\n")
				extraLines = nil
			}
			current = nil
			pendingMeta = nil
			continue
		case "host":
			if current != nil {
				current.ExtraSSHOptions = strings.Join(extraLines, "\n")
				extraLines = nil
			}
			if strings.Contains(val, "*") || strings.Contains(val, "?") {
				current = nil
				pendingMeta = nil
				continue
			}
			h := ParsedHost{Alias: val, Port: 22}
			if pendingMeta != nil {
				applyMetadata(&h, *pendingMeta)
			}
			*hosts = append(*hosts, h)
			current = &(*hosts)[len(*hosts)-1]
			pendingMeta = nil
			continue
		}

		if current == nil {
			pendingMeta = nil
			continue
		}

		switch lkey {
		case "hostname":
			current.Hostname = val
		case "port":
			if p, err := strconv.Atoi(val); err == nil {
				current.Port = p
			}
		case "user":
			current.Username = val
		case "identityfile":
			if current.IdentFile == "" {
				current.IdentFile = expandPath(val)
			}
		case "proxyjump":
			current.ProxyJump = val
		case "proxycommand":
			current.ProxyCommand = val
		case "gssapiauthentication":
			current.GSSAPIAuthentication = parseSSHBool(val)
		case "preferredauthentications":
			current.PreferredAuthentications = splitCSV(val)
		case "forwardagent":
			current.ForwardAgent = parseSSHBool(val)
		case "remotecommand":
			current.RemoteCommand = val
		default:
			extraLines = append(extraLines, trimmed)
		}
	}
	if current != nil {
		current.ExtraSSHOptions = strings.Join(extraLines, "\n")
	}
	return scanner.Err()
}

func parseMetadataComment(line string) (etermMetadata, bool) {
	const prefix = "# eterm:"
	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return etermMetadata{}, false
	}
	var meta etermMetadata
	if err := json.Unmarshal([]byte(strings.TrimSpace(line[len(prefix):])), &meta); err != nil {
		return etermMetadata{}, false
	}
	return meta, true
}

func splitDirective(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	if idx := strings.Index(line, "="); idx >= 0 {
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if key != "" {
			return key, val, true
		}
	}
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", "", false
	}
	key := parts[0]
	val := strings.TrimSpace(line[len(key):])
	return key, val, true
}

func applyMetadata(h *ParsedHost, meta etermMetadata) {
	h.Group = meta.Group
	h.Tags = meta.Tags
	h.Description = meta.Description
	h.AuthMethod = meta.AuthMethod
	h.KeyName = meta.KeyName
	h.ProxyType = meta.ProxyType
	h.ProxyHost = meta.ProxyHost
	h.ProxyPort = meta.ProxyPort
	h.ProxyUser = meta.ProxyUser
	h.GSSAPISource = meta.GSSAPISource
	h.GSSAPIKeytab = meta.GSSAPIKeytab
	h.KrbPrincipal = meta.KrbPrincipal
}

func resolveIncludePaths(baseDir, raw string) []string {
	fields := strings.Fields(raw)
	var out []string
	for _, field := range fields {
		field = strings.Trim(field, `"'`)
		field = expandPath(field)
		if !filepath.IsAbs(field) {
			field = filepath.Join(baseDir, field)
		}
		matches, err := filepath.Glob(field)
		if err != nil || len(matches) == 0 {
			continue
		}
		for _, match := range matches {
			out = append(out, match)
		}
	}
	return out
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func parseSSHBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "true", "on", "1":
		return true
	default:
		return false
	}
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func ValidateExtraOptions(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	modeled := map[string]bool{
		"host": true, "hostname": true, "user": true, "port": true,
		"identityfile": true, "proxyjump": true, "proxycommand": true,
		"gssapiauthentication": true, "preferredauthentications": true,
		"forwardagent": true, "remotecommand": true,
	}
	structural := map[string]bool{"host": true, "match": true, "include": true}
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := splitDirective(trimmed)
		if !ok {
			return fmt.Errorf("line %d: invalid SSH option", i+1)
		}
		lkey := strings.ToLower(strings.TrimSpace(key))
		if structural[lkey] {
			return fmt.Errorf("line %d: %s is not allowed in extra options", i+1, key)
		}
		if modeled[lkey] {
			return fmt.Errorf("line %d: %s is already modeled elsewhere", i+1, key)
		}
	}
	return nil
}

