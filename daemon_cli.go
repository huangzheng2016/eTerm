package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/huangzheng2016/eTerm/internal/config"
	"github.com/huangzheng2016/eTerm/internal/daemon"
)

type daemonOptions struct {
	DBPath   string
	Password string
	Name     string
}

type daemonController struct {
	pidPath string
	logPath string
	isAlive func(int) bool
}

func runDaemon(args []string) {
	cmd, opts, err := parseDaemonArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if cmd == "run" {
		if err := daemon.Run(context.Background(), daemon.Config{
			DBPath:   opts.DBPath,
			Password: opts.Password,
			Name:     opts.Name,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "eterm daemon: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctl, err := newDaemonController()
	if err != nil {
		fmt.Fprintf(os.Stderr, "eterm daemon: %v\n", err)
		os.Exit(1)
	}
	switch cmd {
	case "start":
		os.Exit(ctl.start(os.Stdout, opts))
	case "stop":
		os.Exit(ctl.stop(os.Stdout))
	case "status":
		os.Exit(ctl.status(os.Stdout))
	default:
		fmt.Fprintf(os.Stderr, "unknown daemon command %q\n", cmd)
		os.Exit(2)
	}
}

func parseDaemonArgs(args []string) (string, daemonOptions, error) {
	cmd := "start"
	if len(args) > 0 {
		switch args[0] {
		case "start", "stop", "status", "run":
			cmd = args[0]
			args = args[1:]
		case "-h", "--help":
		default:
			if !strings.HasPrefix(args[0], "-") {
				return "", daemonOptions{}, fmt.Errorf("unknown daemon command %q", args[0])
			}
		}
	}

	fs := flag.NewFlagSet("daemon "+cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("c", "", "path to SQLite database file (default: ~/.config/eterm/eterm.db)")
	password := fs.String("password", "", "master password (env: ETERM_MASTER_PASSWORD)")
	name := fs.String("name", "", "peer display name (default: hostname)")
	if err := fs.Parse(args); err != nil {
		return "", daemonOptions{}, err
	}
	return cmd, daemonOptions{DBPath: *dbPath, Password: *password, Name: *name}, nil
}

func newDaemonController() (daemonController, error) {
	if err := config.EnsureConfigDir(); err != nil {
		return daemonController{}, err
	}
	dir := config.ConfigDir()
	return daemonController{
		pidPath: filepath.Join(dir, "daemon.pid"),
		logPath: filepath.Join(dir, "daemon.log"),
		isAlive: isProcessAlive,
	}, nil
}

func (c daemonController) start(out io.Writer, opts daemonOptions) int {
	if pid, err := c.readPID(); err == nil {
		if c.isAlive(pid) {
			fmt.Fprintf(out, "running pid=%d\n", pid)
			return 0
		}
		_ = os.Remove(c.pidPath)
	}

	logFile, err := os.OpenFile(c.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		fmt.Fprintf(out, "start failed: %v\n", err)
		return 1
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(out, "start failed: %v\n", err)
		return 1
	}
	args := []string{"daemon", "run"}
	if opts.DBPath != "" {
		args = append(args, "-c", opts.DBPath)
	}
	if opts.Password != "" {
		args = append(args, "-password", opts.Password)
	}
	if opts.Name != "" {
		args = append(args, "-name", opts.Name)
	}
	pid, err := startDetachedDaemon(exe, args, logFile)
	if err != nil {
		fmt.Fprintf(out, "start failed: %v\n", err)
		return 1
	}
	if err := os.WriteFile(c.pidPath, []byte(strconv.Itoa(pid)+"\n"), 0600); err != nil {
		fmt.Fprintf(out, "start failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "started pid=%d log=%s\n", pid, c.logPath)
	return 0
}

func (c daemonController) stop(out io.Writer) int {
	pid, err := c.readPID()
	if err != nil {
		fmt.Fprintln(out, "stopped")
		return 0
	}
	if !c.isAlive(pid) {
		_ = os.Remove(c.pidPath)
		fmt.Fprintln(out, "stopped")
		return 0
	}
	if err := terminateProcess(pid); err != nil {
		fmt.Fprintf(out, "stop failed: %v\n", err)
		return 1
	}
	for i := 0; i < 20; i++ {
		if !c.isAlive(pid) {
			_ = os.Remove(c.pidPath)
			fmt.Fprintln(out, "stopped")
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Fprintf(out, "stop failed: pid %d still running\n", pid)
	return 1
}

func (c daemonController) status(out io.Writer) int {
	pid, err := c.readPID()
	if err != nil || !c.isAlive(pid) {
		fmt.Fprintln(out, "stopped")
		return 1
	}
	fmt.Fprintf(out, "running pid=%d\n", pid)
	return 0
}

func (c daemonController) readPID() (int, error) {
	data, err := os.ReadFile(c.pidPath)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, errors.New("invalid pid")
	}
	return pid, nil
}
