//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestWindowsTaskXMLQuotesPathsWithSpaces(t *testing.T) {
	doc := windowsTaskXML([]string{`C:\Program Files\eTerm\eterm.exe`, "daemon", "run", "-c", `C:\Users\A B\eterm.db`})
	for _, want := range []string{
		`<LogonTrigger>`,
		`<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`,
		`<RestartOnFailure>`,
		`<Count>3</Count>`,
		`<Interval>PT1M</Interval>`,
		`<Command>"C:\Program Files\eTerm\eterm.exe"</Command>`,
		`<Arguments>daemon run -c "C:\Users\A B\eterm.db"</Arguments>`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("XML missing %q:\n%s", want, doc)
		}
	}
}

func TestWindowsTaskXMLLeavesPlainPathsUnquoted(t *testing.T) {
	doc := windowsTaskXML([]string{`C:\eTerm\eterm.exe`, "daemon", "run"})
	if !strings.Contains(doc, `<Command>C:\eTerm\eterm.exe</Command>`) {
		t.Fatalf("XML command not unquoted:\n%s", doc)
	}
	if !strings.Contains(doc, `<Arguments>daemon run</Arguments>`) {
		t.Fatalf("XML arguments wrong:\n%s", doc)
	}
}

func TestWindowsTaskXMLEscapesSpecialChars(t *testing.T) {
	doc := windowsTaskXML([]string{`C:\R&D\eterm.exe`, "daemon", "run"})
	if !strings.Contains(doc, `<Command>C:\R&amp;D\eterm.exe</Command>`) {
		t.Fatalf("XML not escaped:\n%s", doc)
	}
}

func TestWindowsCreateTaskInvokesSchtasksWithXML(t *testing.T) {
	const xmlDoc = `<?xml version="1.0" encoding="UTF-16"?><Task/>`
	var gotArgs []string
	var xmlPath string
	var doc string
	restore := stubSchtasks(t, func(args ...string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		xmlPath = args[4]
		data, err := os.ReadFile(xmlPath)
		if err != nil {
			return nil, err
		}
		doc = decodeUTF16LE(t, data)
		return []byte("SUCCESS"), nil
	})
	defer restore()

	if err := windowsCreateTask(xmlDoc); err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"/Create", "/TN", "eTermDaemon", "/XML", xmlPath, "/F"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("schtasks args = %v, want %v", gotArgs, wantArgs)
	}
	if !strings.HasPrefix(xmlPath, filepath.Join(os.TempDir(), "eterm-daemon-")) {
		t.Fatalf("XML path %q not a temp file", xmlPath)
	}
	if _, statErr := os.Stat(xmlPath); !os.IsNotExist(statErr) {
		t.Fatal("temp XML file was not removed")
	}
	if doc != xmlDoc {
		t.Fatalf("XML doc = %q, want %q", doc, xmlDoc)
	}
}

func TestWindowsCreateTaskReportsSchtasksFailure(t *testing.T) {
	restore := stubSchtasks(t, func(args ...string) ([]byte, error) {
		return []byte("ERROR: access denied"), errors.New("exit status 5")
	})
	defer restore()
	err := windowsCreateTask("<Task/>")
	if err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestDaemonServiceEnableRejectsPassword(t *testing.T) {
	called := false
	restore := stubSchtasks(t, func(args ...string) ([]byte, error) {
		called = true
		return nil, nil
	})
	defer restore()
	err := daemonServiceEnable(daemonOptions{Password: "pw"})
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("schtasks was invoked despite password rejection")
	}
}

func TestDaemonServiceDisableDeletesTask(t *testing.T) {
	var gotArgs []string
	restore := stubSchtasks(t, func(args ...string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		return nil, nil
	})
	defer restore()
	if err := daemonServiceDisable(); err != nil {
		t.Fatal(err)
	}
	want := []string{"/Delete", "/TN", "eTermDaemon", "/F"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("schtasks args = %v, want %v", gotArgs, want)
	}
}

func TestDaemonServiceDisableToleratesMissingTask(t *testing.T) {
	for _, out := range []string{
		"ERROR: The system cannot find the file specified.",
		"错误: 找不到指定的文件。",
	} {
		restore := stubSchtasks(t, func(args ...string) ([]byte, error) {
			return []byte(out), errors.New("exit status 1")
		})
		if err := daemonServiceDisable(); err != nil {
			t.Fatalf("output %q: err = %v", out, err)
		}
		restore()
	}
}

func TestDaemonServiceDisableReportsOtherFailures(t *testing.T) {
	restore := stubSchtasks(t, func(args ...string) ([]byte, error) {
		return []byte("ERROR: Access is denied."), errors.New("exit status 1")
	})
	defer restore()
	err := daemonServiceDisable()
	if err == nil || !strings.Contains(err.Error(), "Access is denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestDaemonServiceStatusParsesQueryCSV(t *testing.T) {
	restore := stubSchtasks(t, func(args ...string) ([]byte, error) {
		if !reflect.DeepEqual(args, []string{"/Query", "/TN", "eTermDaemon", "/FO", "CSV", "/NH"}) {
			t.Fatalf("schtasks args = %v", args)
		}
		return []byte(`"\eTermDaemon","N/A","Ready"` + "\r\n"), nil
	})
	defer restore()
	enabled, detail := daemonServiceStatus()
	if !enabled || detail != "enabled (task scheduler eTermDaemon: Ready)" {
		t.Fatalf("enabled = %v detail = %q", enabled, detail)
	}
}

func TestDaemonServiceStatusMissingTask(t *testing.T) {
	restore := stubSchtasks(t, func(args ...string) ([]byte, error) {
		return []byte("ERROR: The system cannot find the file specified."), errors.New("exit status 1")
	})
	defer restore()
	enabled, detail := daemonServiceStatus()
	if enabled || detail != "not installed" {
		t.Fatalf("enabled = %v detail = %q", enabled, detail)
	}
}

func stubSchtasks(t *testing.T, fn func(args ...string) ([]byte, error)) func() {
	t.Helper()
	orig := schtasksRun
	schtasksRun = fn
	return func() { schtasksRun = orig }
}

func decodeUTF16LE(t *testing.T, data []byte) string {
	t.Helper()
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xFE {
		t.Fatalf("missing UTF-16 LE BOM: % x", data[:min(4, len(data))])
	}
	u := make([]uint16, (len(data)-2)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(data[2+2*i:])
	}
	return string(utf16.Decode(u))
}
