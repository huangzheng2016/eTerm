package localterm

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const SettingShell = "local_terminal_shell"

var shellProbeDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"}

func ResolveShell(configured string, exists func(string) bool) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return absolutizeShell(configured, exists)
	}
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" && runtime.GOOS != "windows" {
		return absolutizeShell(shell, exists)
	}
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{"pwsh.exe", "powershell.exe", "cmd.exe"} {
			if _, err := exec.LookPath(candidate); err == nil {
				return candidate
			}
		}
		return "cmd.exe"
	}
	if shell := loginShell(); shell != "" && exists(shell) {
		return shell
	}
	if exists("/bin/zsh") {
		return "/bin/zsh"
	}
	if exists("/bin/bash") {
		return "/bin/bash"
	}
	return "sh"
}

func absolutizeShell(shell string, exists func(string) bool) string {
	if runtime.GOOS == "windows" || strings.Contains(shell, "/") {
		return shell
	}
	if _, err := exec.LookPath(shell); err == nil {
		return shell
	}
	for _, dir := range shellProbeDirs {
		path := filepath.Join(dir, shell)
		if exists(path) {
			return path
		}
	}
	return shell
}

var loginShell = detectLoginShell

func detectLoginShell() string {
	user := os.Getenv("USER")
	if user == "" {
		return ""
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("/usr/bin/dscl", ".", "-read", "/Users/"+user, "UserShell").Output()
		if err != nil {
			return ""
		}
		rest, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "UserShell:")
		if !ok {
			return ""
		}
		return strings.TrimSpace(rest)
	}
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	for line := range strings.Lines(string(data)) {
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[0] == user {
			return strings.TrimSpace(fields[6])
		}
	}
	return ""
}

func DefaultShell(configured string) string {
	return ResolveShell(configured, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
}
