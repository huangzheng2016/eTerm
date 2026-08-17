package localterm

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const SettingShell = "local_terminal_shell"

func ResolveShell(configured string, exists func(string) bool) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured
	}
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" && runtime.GOOS != "windows" {
		return shell
	}
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{"pwsh.exe", "powershell.exe", "cmd.exe"} {
			if _, err := exec.LookPath(candidate); err == nil {
				return candidate
			}
		}
		return "cmd.exe"
	}
	if exists("/bin/zsh") {
		return "/bin/zsh"
	}
	if exists("/bin/bash") {
		return "/bin/bash"
	}
	return "sh"
}

func DefaultShell(configured string) string {
	return ResolveShell(configured, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
}
