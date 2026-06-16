package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/app"
	"github.com/huangzheng2016/eTerm/internal/config"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/login"
	"github.com/huangzheng2016/eTerm/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		runDaemon(os.Args[2:])
		return
	}

	dbPathFlag := flag.String("c", "", "path to SQLite database file (default: ~/.config/eterm/eterm.db)")
	portFlag := flag.Int("p", 0, "SSH port (used with direct connect: eterm [user@]host [-p port])")
	versionFlag := flag.Bool("v", false, "print version and exit")
	versionJSONFlag := flag.Bool("version-json", false, "print version and commit as JSON and exit")
	noUpdateCheckFlag := flag.Bool("no-update-check", false, "disable GitHub release check on unlock")
	forceUpdateCheck, cliArgs := splitUpgradeCommand(os.Args[1:])
	flag.CommandLine.Parse(cliArgs)

	if *versionJSONFlag {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{
			"version": version.Version,
			"commit":  version.Commit,
		})
		os.Exit(0)
	}

	if *versionFlag {
		fmt.Println("eTerm " + version.Version + " (" + version.Commit + ")")
		os.Exit(0)
	}

	if os.Getenv("ETERM_DEBUG_KEYS") != "" {
		fmt.Fprintln(os.Stderr, "eterm: ETERM_DEBUG_KEYS is on — each key on the connection list is logged to stderr (String / Keystroke / Code).")
	}
	if os.Getenv("ETERM_DEBUG_APP") != "" {
		fmt.Fprintln(os.Stderr, "eterm: ETERM_DEBUG_APP is on — SSH/SFTP connect steps are logged to stderr ([eterm:app]).")
	}

	dbPath := config.DBPath()
	if *dbPathFlag != "" {
		var err error
		dbPath, err = filepath.Abs(*dbPathFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to resolve database path: %v\n", err)
			os.Exit(1)
		}
	}

	if err := config.EnsureConfigDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create config directory: %v\n", err)
		os.Exit(1)
	}

	database, err := db.InitDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize database: %v\n", err)
		os.Exit(1)
	}

	var masterKey *security.MasterKeyManager
	isSetup := false
	noPassword := false

	saltStr, err := db.GetSetting(database, "encryption_salt")
	if err != nil {
		isSetup = true
		masterKey = security.NewMasterKeyManager(nil, nil, 30*time.Minute)
	} else {
		salt, err := base64.StdEncoding.DecodeString(saltStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to decode salt: %v\n", err)
			os.Exit(1)
		}

		verifierStr, err := db.GetSetting(database, "encryption_verifier")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load verifier: %v\n", err)
			os.Exit(1)
		}

		verifier, err := base64.StdEncoding.DecodeString(verifierStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to decode verifier: %v\n", err)
			os.Exit(1)
		}

		masterKey = security.NewMasterKeyManager(salt, verifier, 30*time.Minute)

		npStr, npErr := db.GetSetting(database, "no_password")
		if npErr == nil && npStr == "true" {
			noPassword = true
		}
	}

	noUpdateCheck := *noUpdateCheckFlag || os.Getenv("ETERM_NO_UPDATE_CHECK") != ""

	var a app.App
	if noPassword {
		masterKey.UnlockNoPassword()
		a = app.NewApp(database, masterKey).SetInitCmd(func() tea.Msg {
			return types.MasterKeyUnlockedMsg{NoPassword: true}
		}).SetNoUpdateCheck(noUpdateCheck).SetForceUpdateCheck(forceUpdateCheck)
	} else {
		loginModel := login.New(masterKey, isSetup)
		a = app.NewApp(database, masterKey).SetLoginModel(loginModel).SetNoUpdateCheck(noUpdateCheck).SetForceUpdateCheck(forceUpdateCheck)
	}

	// CLI direct connect: eterm [user@]host[:port] [-p port]
	if args := flag.Args(); len(args) > 0 {
		hostname, port, username := parseQuickConnect(args[0])
		if *portFlag > 0 {
			port = *portFlag
		}
		a = a.SetPendingCLIConnect(hostname, username, port)
	}

	p := tea.NewProgram(a)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}

	masterKey.Lock()
}

func splitUpgradeCommand(args []string) (bool, []string) {
	if len(args) == 0 || args[0] != "upgrade" {
		return false, args
	}
	return true, args[1:]
}

// parseQuickConnect parses [user@]host[:port] into components.
func parseQuickConnect(raw string) (hostname string, port int, username string) {
	port = 22
	username = "root"
	if at := strings.Index(raw, "@"); at >= 0 {
		username = raw[:at]
		raw = raw[at+1:]
	}
	if colon := strings.LastIndex(raw, ":"); colon >= 0 {
		if p, err := strconv.Atoi(raw[colon+1:]); err == nil && p > 0 && p < 65536 {
			port = p
			raw = raw[:colon]
		}
	}
	hostname = raw
	return
}
