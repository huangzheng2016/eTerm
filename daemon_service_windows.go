//go:build windows

package main

import (
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"
)

const windowsTaskName = "eTermDaemon"

var schtasksRun = func(args ...string) ([]byte, error) {
	return exec.Command("schtasks", args...).CombinedOutput()
}

func daemonServiceEnable(opts daemonOptions) error {
	if err := daemonServiceCheckNoPassword(opts); err != nil {
		return err
	}
	args, err := daemonServiceProgramArguments(opts)
	if err != nil {
		return err
	}
	return windowsCreateTask(windowsTaskXML(args))
}

func daemonServiceDisable() error {
	out, err := schtasksRun("/Delete", "/TN", windowsTaskName, "/F")
	if err != nil {
		return fmt.Errorf("schtasks /Delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func daemonServiceStatus() (bool, string) {
	out, err := schtasksRun("/Query", "/TN", windowsTaskName, "/FO", "CSV", "/NH")
	if err != nil {
		return false, "not installed"
	}
	detail := "enabled (task scheduler " + windowsTaskName + ")"
	if fields, err := csv.NewReader(strings.NewReader(string(out))).Read(); err == nil && len(fields) >= 3 {
		if status := strings.TrimSpace(fields[2]); status != "" {
			detail = "enabled (task scheduler " + windowsTaskName + ": " + status + ")"
		}
	}
	return true, detail
}

func windowsCreateTask(xmlDoc string) error {
	f, err := os.CreateTemp("", "eterm-daemon-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(windowsUTF16LE(xmlDoc)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	out, err := schtasksRun("/Create", "/TN", windowsTaskName, "/XML", f.Name(), "/F")
	if err != nil {
		return fmt.Errorf("schtasks /Create: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func windowsTaskXML(programArgs []string) string {
	quoted := make([]string, 0, len(programArgs)-1)
	for _, a := range programArgs[1:] {
		quoted = append(quoted, windowsXMLEscape(windowsQuoteArg(a)))
	}
	return fmt.Sprintf(windowsTaskXMLTemplate, windowsXMLEscape(windowsQuoteArg(programArgs[0])), strings.Join(quoted, " "))
}

func windowsQuoteArg(s string) string {
	if strings.ContainsAny(s, " \t") {
		return "\"" + s + "\""
	}
	return s
}

func windowsXMLEscape(s string) string {
	return windowsXMLReplacer.Replace(s)
}

var windowsXMLReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func windowsUTF16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	buf := make([]byte, 2+2*len(u))
	buf[0] = 0xFF
	buf[1] = 0xFE
	for i, c := range u {
		binary.LittleEndian.PutUint16(buf[2+2*i:], c)
	}
	return buf
}

const windowsTaskXMLTemplate = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>eTerm sync daemon</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RestartOnFailure>
      <Count>3</Count>
      <Interval>PT1M</Interval>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>%s</Arguments>
    </Exec>
  </Actions>
</Task>
`
