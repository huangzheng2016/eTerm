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

func EnsureRemoteConfig(client *ssh.Client, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured, nil
	}
	out, err := internalssh.RunCommand(client, "cat "+RemoteConfigPath+" 2>/dev/null", "")
	if err == nil && string(out) == ManagedConfig {
		return RemoteConfigPath, nil
	}
	cmd := fmt.Sprintf("umask 077 && mkdir -p %s && cat > %s", remoteConfigDir, RemoteConfigPath)
	if _, err := internalssh.RunCommand(client, cmd, ManagedConfig); err != nil {
		return "", err
	}
	return RemoteConfigPath, nil
}

func RemoteAttachCommand(configFile, name string) string {
	return remoteTmuxCmd(configFile, "attach-session -t "+quoteShell(name))
}

func RemoteNewCommand(configFile, name string) string {
	return remoteTmuxCmd(configFile, "new-session -A -s "+quoteShell(name))
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
