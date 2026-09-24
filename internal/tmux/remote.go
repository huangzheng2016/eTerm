package tmux

import (
	"errors"
	"fmt"
	"strings"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"golang.org/x/crypto/ssh"
)

const RemoteConfigPath = "$HOME/.config/eterm/tmux.conf"

const remoteConfigDir = "$HOME/.config/eterm"

var ErrRemoteTmuxNotFound = errors.New("tmux is not installed on the remote host")

func ProbeRemote(client *ssh.Client) error {
	out, err := internalssh.RunCommand(client, "command -v tmux", "")
	if err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			return ErrRemoteTmuxNotFound
		}
		return err
	}
	if strings.TrimSpace(string(out)) == "" {
		return ErrRemoteTmuxNotFound
	}
	return nil
}

func ListRemoteSessions(client *ssh.Client, configFile string) ([]types.TmuxSession, error) {
	out, err := internalssh.RunCommand(client, remoteTmuxCmd(configFile, "list-sessions -F "+quoteShell(listFormat)), "")
	if err != nil {
		if isNoServerOutput(out) {
			return nil, nil
		}
		return nil, tmuxCommandError("list-sessions", err, out)
	}
	return parseSessions(out), nil
}

func KillRemoteSession(client *ssh.Client, configFile, name string) error {
	out, err := internalssh.RunCommand(client, remoteTmuxCmd(configFile, "kill-session -t "+quoteShell(name)), "")
	if err != nil {
		return tmuxCommandError("kill-session", err, out)
	}
	return nil
}

func RenameRemoteSession(client *ssh.Client, configFile, oldName, newName string) error {
	out, err := internalssh.RunCommand(client, remoteTmuxCmd(configFile, "rename-session -t "+quoteShell(oldName)+" "+quoteShell(newName)), "")
	if err != nil {
		return tmuxCommandError("rename-session", err, out)
	}
	return nil
}

func EnsureRemoteConfig(client *ssh.Client, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured, nil
	}
	out, readErr := internalssh.RunCommand(client, "cat "+RemoteConfigPath+" 2>/dev/null", "")
	if readErr == nil && string(out) == ManagedConfig {
		return RemoteConfigPath, nil
	}
	cmd := fmt.Sprintf("umask 077 && mkdir -p %s && cat > %s", remoteConfigDir, RemoteConfigPath)
	_, writeErr := internalssh.RunCommand(client, cmd, ManagedConfig)
	if writeErr == nil {
		return RemoteConfigPath, nil
	}
	tmpPath, tmpErr := writeRemoteTempConfig(client)
	if tmpErr != nil {
		return "", fmt.Errorf("write %s: %w; temporary fallback: %v", RemoteConfigPath, writeErr, tmpErr)
	}
	return tmpPath, nil
}

func writeRemoteTempConfig(client *ssh.Client) (string, error) {
	out, err := internalssh.RunCommand(client, remoteTempConfigCommand(), ManagedConfig)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", errors.New("remote temporary tmux config path is empty")
	}
	return path, nil
}

func remoteTempConfigCommand() string {
	return `tmp=$(mktemp /tmp/eterm-tmux.XXXXXXXX) && chmod 600 "$tmp" && cat > "$tmp" && printf '%s\n' "$tmp"`
}

func RemoteAttachCommand(configFile, name string) string {
	return "exec " + remoteTmuxCmd(configFile, "attach-session -t "+quoteShell(name))
}

func RemoteNewCommand(configFile, name string) string {
	return "exec " + remoteTmuxCmd(configFile, "new-session -A -s "+quoteShell(name))
}

func remoteTmuxCmd(configFile, args string) string {
	return "tmux" + remoteConfigArg(configFile) + " " + args
}

func remoteConfigArg(configFile string) string {
	if configFile == "" {
		return ""
	}
	if strings.HasPrefix(configFile, "~") || strings.HasPrefix(configFile, "$") {
		return " -f " + configFile
	}
	return " -f " + quoteShell(configFile)
}

func quoteShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
