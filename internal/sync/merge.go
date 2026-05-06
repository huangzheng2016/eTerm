package sync

import (
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

// MergeResult holds merge statistics.
type MergeResult struct {
	Merged int
	Failed int
}

// MergeRecords applies pulled records to the local database inside a transaction.
func MergeRecords(database *gorm.DB, mk *security.MasterKeyManager, passphrase string, records []SyncRecord) MergeResult {
	var keys, hosts, fwds, snippets []SyncRecord
	for _, r := range records {
		switch r.Type {
		case TypeSSHKey:
			keys = append(keys, r)
		case TypeHost:
			hosts = append(hosts, r)
		case TypePortFwd:
			fwds = append(fwds, r)
		case TypeSnippet:
			snippets = append(snippets, r)
		}
	}

	var res MergeResult
	database.Transaction(func(tx *gorm.DB) error {
		r := mergeSSHKeys(tx, mk, passphrase, keys)
		res.Merged += r.Merged
		res.Failed += r.Failed
		r = mergeHosts(tx, mk, passphrase, hosts)
		res.Merged += r.Merged
		res.Failed += r.Failed
		r = mergePortForwards(tx, passphrase, fwds)
		res.Merged += r.Merged
		res.Failed += r.Failed
		r = mergeSnippets(tx, passphrase, snippets)
		res.Merged += r.Merged
		res.Failed += r.Failed
		return nil
	})
	return res
}
