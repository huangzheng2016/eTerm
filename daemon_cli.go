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
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/debugpprof"
)

type daemonOptions struct {
	DBPath      string
	Password    string
	Name        string
	PProfAddr   string
	Positionals []string
}

type daemonController struct {
	pidPath   string
	lockPath  string
	logPath   string
	isAlive   func(int) bool
	terminate func(int) error
	kill      func(int) error
}

var errDaemonAlreadyRunning = errors.New("daemon already running")

const exitCodeDaemonAlreadyRunning = 3

func daemonLockPath() string {
	return filepath.Join(config.ConfigDir(), "daemon.lock")
}

func acquireDaemonLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockDaemonFile(f); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func readDaemonLockPid(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func daemonLockHolder(path string) (int, bool) {
	f, err := acquireDaemonLock(path)
	if err == nil {
		f.Close()
		return 0, false
	}
	if !errors.Is(err, errDaemonAlreadyRunning) {
		return 0, false
	}
	return readDaemonLockPid(path), true
}

func daemonEnableGuard(lockPath string) error {
	pid, held := daemonLockHolder(lockPath)
	if !held {
		return nil
	}
	if pid > 0 {
		return fmt.Errorf("eterm daemon already running pid=%d (log %s): stop it first ('eterm daemon stop' or 'eterm daemon disable')", pid, daemonServiceLogPath())
	}
	return fmt.Errorf("eterm daemon already running (log %s): stop it first ('eterm daemon stop' or 'eterm daemon disable')", daemonServiceLogPath())
}

func runDaemon(args []string) {
	cmd, opts, err := parseDaemonArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if cmd == "run" {
		if err := config.EnsureConfigDir(); err != nil {
			fmt.Fprintf(os.Stderr, "eterm daemon: %v\n", err)
			os.Exit(1)
		}
		lockPath := daemonLockPath()
		lock, err := acquireDaemonLock(lockPath)
		if errors.Is(err, errDaemonAlreadyRunning) {
			if pid := readDaemonLockPid(lockPath); pid > 0 {
				fmt.Fprintf(os.Stderr, "eterm daemon already running pid=%d (log %s)\n", pid, daemonServiceLogPath())
			} else {
				fmt.Fprintf(os.Stderr, "eterm daemon already running (log %s)\n", daemonServiceLogPath())
			}
			os.Exit(exitCodeDaemonAlreadyRunning)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "eterm daemon: %v\n", err)
			os.Exit(1)
		}
		defer lock.Close()
		if _, err := debugpprof.Start("eterm-daemon", debugpprof.ResolveAddr(opts.PProfAddr, "ETERM_DAEMON_PPROF_ADDR")); err != nil {
			fmt.Fprintf(os.Stderr, "eterm daemon: pprof: %v\n", err)
			os.Exit(1)
		}
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
		code := ctl.status(os.Stdout)
		_, detail := daemonServiceStatus()
		fmt.Fprintf(os.Stdout, "service: %s\n", detail)
		os.Exit(code)
	case "rename":
		os.Exit(renameDaemonPeer(os.Stdout, opts))
	case "enable":
		if err := daemonEnableGuard(daemonLockPath()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := daemonServiceEnable(opts); err != nil {
			fmt.Fprintf(os.Stderr, "eterm daemon enable: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "service enabled")
	case "disable":
		if err := daemonServiceDisable(); err != nil {
			fmt.Fprintf(os.Stderr, "eterm daemon disable: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "service disabled")
	default:
		fmt.Fprintf(os.Stderr, "unknown daemon command %q\n", cmd)
		os.Exit(2)
	}
}

func parseDaemonArgs(args []string) (string, daemonOptions, error) {
	cmd := "start"
	if len(args) > 0 {
		switch args[0] {
		case "start", "stop", "status", "run", "enable", "disable", "rename":
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
	pprofAddr := fs.String("pprof", "", "enable pprof HTTP server on address (env: ETERM_DAEMON_PPROF_ADDR)")
	if err := fs.Parse(args); err != nil {
		return "", daemonOptions{}, err
	}
	return cmd, daemonOptions{DBPath: *dbPath, Password: *password, Name: *name, PProfAddr: *pprofAddr, Positionals: fs.Args()}, nil
}

func renameDaemonPeer(out io.Writer, opts daemonOptions) int {
	if len(opts.Positionals) != 1 || strings.TrimSpace(opts.Positionals[0]) == "" {
		fmt.Fprintln(out, "usage: eterm daemon rename <name>")
		return 2
	}
	name := strings.TrimSpace(opts.Positionals[0])
	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = config.DBPath()
	}
	database, err := db.InitDB(dbPath)
	if err != nil {
		fmt.Fprintf(out, "rename failed: %v\n", err)
		return 1
	}
	if err := db.SetSetting(database, "daemon_peer_name", name); err != nil {
		fmt.Fprintf(out, "rename failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "peer name set to %q; a running daemon picks it up within a few seconds\n", name)
	return 0
}

func newDaemonController() (daemonController, error) {
	if err := config.EnsureConfigDir(); err != nil {
		return daemonController{}, err
	}
	dir := config.ConfigDir()
	return daemonController{
		pidPath:   filepath.Join(dir, "daemon.pid"),
		lockPath:  daemonLockPath(),
		logPath:   filepath.Join(dir, "daemon.log"),
		isAlive:   isProcessAlive,
		terminate: terminateProcess,
		kill:      killProcess,
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
	if c.lockPath != "" {
		if pid, held := daemonLockHolder(c.lockPath); held {
			if pid > 0 {
				fmt.Fprintf(out, "running pid=%d\n", pid)
			} else {
				fmt.Fprintln(out, "running")
			}
			return 0
		}
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
	if opts.Name != "" {
		args = append(args, "-name", opts.Name)
	}
	if opts.PProfAddr != "" {
		args = append(args, "-pprof", opts.PProfAddr)
	}
	var env []string
	if opts.Password != "" {
		env = append(env, "ETERM_MASTER_PASSWORD="+opts.Password)
	}
	pid, err := startDetachedDaemon(exe, args, env, logFile)
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
	if err := c.terminate(pid); err != nil {
		fmt.Fprintf(out, "stop failed: %v\n", err)
		return 1
	}
	if c.waitGone(pid, 2*time.Second) {
		_ = os.Remove(c.pidPath)
		fmt.Fprintln(out, "stopped")
		return 0
	}
	if err := c.kill(pid); err != nil {
		fmt.Fprintf(out, "stop failed: %v\n", err)
		return 1
	}
	if c.waitGone(pid, time.Second) {
		_ = os.Remove(c.pidPath)
		fmt.Fprintln(out, "stopped")
		return 0
	}
	fmt.Fprintf(out, "stop failed: pid %d still running\n", pid)
	return 1
}

func (c daemonController) waitGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !c.isAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
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
