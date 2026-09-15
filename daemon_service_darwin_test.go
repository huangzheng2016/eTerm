//go:build darwin

package main

import (
	"strings"
	"testing"
)

func TestDaemonLaunchdPlistContents(t *testing.T) {
	plist := daemonLaunchdPlist([]string{"/usr/local/bin/eterm", "daemon", "run", "-c", "/tmp/e&t.db", "-name", "box", "-pprof", "127.0.0.1:6061"}, "/Users/u/.config/eterm/daemon.log")
	for _, want := range []string{
		"<key>Label</key>\n\t<string>com.huangzheng2016.eterm.daemon</string>",
		"<key>ProgramArguments</key>",
		"<string>/usr/local/bin/eterm</string>",
		"<string>daemon</string>",
		"<string>run</string>",
		"<string>-c</string>",
		"<string>/tmp/e&amp;t.db</string>",
		"<string>-name</string>",
		"<string>box</string>",
		"<string>-pprof</string>",
		"<string>127.0.0.1:6061</string>",
		"<key>EnvironmentVariables</key>",
		"<key>PATH</key>\n\t\t<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>",
		"<key>LANG</key>\n\t\t<string>en_US.UTF-8</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>KeepAlive</key>\n\t<true/>",
		"<key>StandardOutPath</key>\n\t<string>/Users/u/.config/eterm/daemon.log</string>",
		"<key>StandardErrorPath</key>\n\t<string>/Users/u/.config/eterm/daemon.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist missing %q:\n%s", want, plist)
		}
	}
}

func TestDaemonLaunchdPlistPath(t *testing.T) {
	path, err := daemonLaunchdPlistPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "Library/LaunchAgents/com.huangzheng2016.eterm.daemon.plist") {
		t.Fatalf("path = %q", path)
	}
}
