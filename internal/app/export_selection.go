package app

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/sshconfig"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/termius_exporter/pkg/parser"
	"gorm.io/gorm"
)

type openExportConfigMsg struct{}

func newExportLists(database *gorm.DB) (*importHostListModel, error) {
	var hosts []db.Host
	if err := database.Preload("Key").Order("alias").Find(&hosts).Error; err != nil {
		return nil, err
	}
	existing := make(map[string]bool)
	if parsed, err := sshconfig.ParseSSHConfig(sshconfig.ManagedIncludePath()); err == nil {
		for _, host := range parsed {
			existing[host.Alias] = true
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	items := make([]importHostEntry, 0, len(hosts))
	keys := make([]parser.KeyRecord, 0)
	seenKeys := make(map[uint]bool)
	for _, host := range hosts {
		keyName := ""
		if host.KeyID != nil {
			keyName = host.Key.Name
			if !seenKeys[*host.KeyID] {
				keys = append(keys, parser.KeyRecord{Aliases: []string{keyName}, ID: int(*host.KeyID)})
				seenKeys[*host.KeyID] = true
			}
		}
		items = append(items, importHostEntry{
			rec:         parser.HostRecord{Aliases: []string{host.Alias}, Host: host.Hostname, Port: host.Port, Username: host.Username, KeyName: keyName},
			chosenAlias: host.Alias, selected: true, exportID: host.ID, existing: existing[host.Alias],
		})
	}
	model := newImportHostList(items)
	model.allKeys = keys
	model.exportMode = true
	return model, nil
}

func buildExportKeyItems(hosts []importHostEntry, records []parser.KeyRecord) []importKeyEntry {
	needed := make(map[string]bool)
	for _, host := range hosts {
		if host.selected && host.rec.KeyName != "" {
			needed[host.rec.KeyName] = true
		}
	}
	items := make([]importKeyEntry, 0, len(records))
	for _, record := range records {
		name := record.Aliases[0]
		if !needed[name] {
			continue
		}
		items = append(items, importKeyEntry{rec: record, chosenAlias: name, selected: needed[name], locked: needed[name], exportID: uint(record.ID)})
	}
	return items
}

func runSelectedExport(database *gorm.DB, hosts []importHostEntry) tea.Cmd {
	return func() tea.Msg {
		ids := make([]uint, 0)
		for _, host := range hosts {
			if host.selected {
				ids = append(ids, host.exportID)
			}
		}
		path, err := sshconfig.ExportConfigByHostIDs(database, ids)
		return types.ExportConfigResultMsg{Path: path, Err: err}
	}
}
