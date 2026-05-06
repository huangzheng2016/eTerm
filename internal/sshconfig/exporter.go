package sshconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"gorm.io/gorm"
)

const managedIncludeName = "eterm_hosts.conf"

var nowFunc = time.Now
var execCommand = exec.Command

func MainConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}

func ManagedIncludePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", managedIncludeName)
}

func ExportConfig(database *gorm.DB) (string, error) {
	return ExportConfigToPaths(database, MainConfigPath(), ManagedIncludePath())
}

func ExportConfigToPaths(database *gorm.DB, mainPath, includePath string) (string, error) {
	var hosts []db.Host
	if err := database.Preload("Key").Preload("JumpHost").Find(&hosts).Error; err != nil {
		return "", err
	}
	sort.Slice(hosts, func(i, j int) bool {
		li := strings.TrimSpace(hosts[i].Alias)
		lj := strings.TrimSpace(hosts[j].Alias)
		if li == "" {
			li = hosts[i].Hostname
		}
		if lj == "" {
			lj = hosts[j].Hostname
		}
		return li < lj
	})

	includeBody, err := renderManagedInclude(hosts)
	if err != nil {
		return "", err
	}
	mainBody, err := readOrEmpty(mainPath)
	if err != nil {
		return "", err
	}
	newMain := ensureIncludeLine(mainBody, includePath)

	if err := os.MkdirAll(filepath.Dir(mainPath), 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(includePath), 0o700); err != nil {
		return "", err
	}

	if err := validateConfig(newMain, includeBody, mainPath); err != nil {
		return "", err
	}

	if err := backupIfExists(includePath); err != nil {
		return "", err
	}
	if strings.TrimRight(mainBody, "\n") != strings.TrimRight(newMain, "\n") {
		if err := backupIfExists(mainPath); err != nil {
			return "", err
		}
	}

	if err := writeAtomic(includePath, []byte(includeBody), 0o600); err != nil {
		return "", err
	}
	if err := writeAtomic(mainPath, []byte(newMain), 0o600); err != nil {
		return "", err
	}
	return mainPath, nil
}

func renderManagedInclude(hosts []db.Host) (string, error) {
	var buf bytes.Buffer
	for i, h := range hosts {
		if i > 0 {
			buf.WriteByte('\n')
		}
		meta := etermMetadata{
			Group:        h.Group,
			Tags:         h.Tags,
			Description:  h.Description,
			AuthMethod:   h.AuthMethod,
			ProxyType:    h.ProxyType,
			ProxyHost:    h.ProxyHost,
			ProxyPort:    h.ProxyPort,
			ProxyUser:    h.ProxyUser,
			GSSAPISource: h.GSSAPISource,
			GSSAPIKeytab: h.GSSAPIKeytab,
			KrbPrincipal: h.KrbPrincipal,
		}
		if h.KeyID != nil && h.Key.Name != "" {
			meta.KeyName = h.Key.Name
		}
		data, err := json.Marshal(meta)
		if err != nil {
			return "", err
		}
		buf.WriteString("# eterm: ")
		buf.Write(data)
		buf.WriteByte('\n')

		alias := strings.TrimSpace(h.Alias)
		if alias == "" {
			alias = h.Hostname
		}
		buf.WriteString("Host ")
		buf.WriteString(alias)
		buf.WriteByte('\n')
		writeDirective(&buf, "HostName", h.Hostname)
		writeDirective(&buf, "User", h.Username)
		if h.Port > 0 {
			writeDirective(&buf, "Port", fmt.Sprintf("%d", h.Port))
		}
		if h.ForwardAgent {
			writeDirective(&buf, "ForwardAgent", "yes")
		}
		if strings.TrimSpace(h.RemoteCommand) != "" {
			writeDirective(&buf, "RemoteCommand", h.RemoteCommand)
		}
		if strings.TrimSpace(h.ProxyCommand) != "" {
			writeDirective(&buf, "ProxyCommand", h.ProxyCommand)
		} else if h.JumpHost != nil {
			jumpAlias := strings.TrimSpace(h.JumpHost.Alias)
			if jumpAlias == "" {
				jumpAlias = h.JumpHost.Hostname
			}
			writeDirective(&buf, "ProxyJump", jumpAlias)
		}
		if h.KeyID != nil && h.Key.PrivatePath != "" {
			writeDirective(&buf, "IdentityFile", h.Key.PrivatePath)
		}
		if h.AuthMethod == "gssapi" {
			writeDirective(&buf, "GSSAPIAuthentication", "yes")
			writeDirective(&buf, "PreferredAuthentications", "gssapi-with-mic")
		} else {
			if pref := preferredAuthDirective(h.AuthMethod); pref != "" {
				writeDirective(&buf, "PreferredAuthentications", pref)
			}
		}
		for _, line := range strings.Split(h.ExtraSSHOptions, "\n") {
			line = strings.TrimRight(line, " \t")
			if strings.TrimSpace(line) == "" {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				buf.WriteString("  ")
				buf.WriteString(strings.TrimSpace(line))
				buf.WriteByte('\n')
				continue
			}
			buf.WriteString("  ")
			buf.WriteString(strings.TrimSpace(line))
			buf.WriteByte('\n')
		}
	}
	return buf.String(), nil
}

func preferredAuthDirective(authMethod string) string {
	switch authMethod {
	case "key":
		return "publickey"
	case "agent":
		return "publickey"
	case "password":
		return "password"
	case "interactive":
		return "keyboard-interactive"
	default:
		return ""
	}
}

func writeDirective(buf *bytes.Buffer, key, val string) {
	if strings.TrimSpace(val) == "" {
		return
	}
	buf.WriteString("  ")
	buf.WriteString(key)
	buf.WriteByte(' ')
	buf.WriteString(val)
	buf.WriteByte('\n')
}

func readOrEmpty(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), nil
	}
	if os.IsNotExist(err) {
		return "", nil
	}
	return "", err
}

func ensureIncludeLine(mainBody, includePath string) string {
	lines := splitLines(mainBody)
	for _, line := range lines {
		key, val, ok := splitDirective(strings.TrimSpace(line))
		if !ok || !strings.EqualFold(key, "Include") {
			continue
		}
		for _, field := range strings.Fields(val) {
			if samePath(expandPath(strings.Trim(field, `"'`)), includePath) {
				return joinLines(lines)
			}
		}
	}
	insertAt := 0
	for insertAt < len(lines) {
		trimmed := strings.TrimSpace(lines[insertAt])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			insertAt++
			continue
		}
		break
	}
	includeLine := "Include " + includePath
	lines = append(lines[:insertAt], append([]string{includeLine}, lines[insertAt:]...)...)
	return joinLines(lines)
}

func validateConfig(mainBody, includeBody, mainPath string) error {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh not found for config validation: %w", err)
	}
	mainDir := filepath.Dir(mainPath)
	tmpInclude, err := os.CreateTemp(mainDir, "eterm-include-*.conf")
	if err != nil {
		return err
	}
	tmpIncludePath := tmpInclude.Name()
	defer os.Remove(tmpIncludePath)
	if _, err := tmpInclude.WriteString(includeBody); err != nil {
		tmpInclude.Close()
		return err
	}
	if err := tmpInclude.Close(); err != nil {
		return err
	}

	tmpMain, err := os.CreateTemp(mainDir, "eterm-main-*.conf")
	if err != nil {
		return err
	}
	tmpMainPath := tmpMain.Name()
	defer os.Remove(tmpMainPath)
	if _, err := tmpMain.WriteString("Include " + tmpIncludePath + "\n" + mainBody); err != nil {
		tmpMain.Close()
		return err
	}
	if err := tmpMain.Close(); err != nil {
		return err
	}

	cmd := execCommand(sshPath, "-G", "-F", tmpMainPath, "dummy")
	cmd.Stdout = &bytes.Buffer{}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh config validation failed: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func backupIfExists(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	backupPath := fmt.Sprintf("%s.bak-%s", path, nowFunc().Format("20060102150405"))
	return os.WriteFile(backupPath, data, 0o600)
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".eterm-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return filepath.Clean(aa) == filepath.Clean(bb)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
