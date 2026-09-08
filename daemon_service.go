package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/huangzheng2016/eTerm/internal/config"
	"github.com/huangzheng2016/eTerm/internal/db"
)

func daemonServiceLogPath() string {
	return filepath.Join(config.ConfigDir(), "daemon.log")
}

func daemonServiceProgramArguments(opts daemonOptions) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return nil, err
	}
	args := []string{exe, "daemon", "run"}
	if opts.DBPath != "" {
		args = append(args, "-c", opts.DBPath)
	}
	return args, nil
}

func daemonServiceCheckNoPassword(opts daemonOptions) error {
	if opts.Password != "" {
		return errors.New("daemon service cannot store a master password: remove -password and switch the database to no-password mode instead")
	}
	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = config.DBPath()
	}
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("database %s is not initialized: run eTerm once before enabling the daemon service", dbPath)
	}
	database, err := db.InitDB(dbPath)
	if err != nil {
		return err
	}
	if sqlDB, err := database.DB(); err == nil {
		defer sqlDB.Close()
	}
	noPassword, _ := db.GetSetting(database, "no_password")
	if noPassword != "true" {
		return errors.New("daemon service requires no-password mode: the master password cannot be stored in the login service; disable the master password in eTerm settings first")
	}
	return nil
}
