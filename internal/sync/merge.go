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
func MergeRecords(database *gorm.DB, mk *security.MasterKeyManager, passphrase string, records []SyncRecord) (MergeResult, error) {
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
	err := database.Transaction(func(tx *gorm.DB) error {
		r, err := mergeSSHKeys(tx, mk, passphrase, keys)
		res.Merged += r.Merged
		res.Failed += r.Failed
		if err != nil {
			return err
		}
		r, err = mergeHosts(tx, mk, passphrase, hosts)
		res.Merged += r.Merged
		res.Failed += r.Failed
		if err != nil {
			return err
		}
		r, err = mergePortForwards(tx, passphrase, fwds)
		res.Merged += r.Merged
		res.Failed += r.Failed
		if err != nil {
			return err
		}
		r, err = mergeSnippets(tx, passphrase, snippets)
		res.Merged += r.Merged
		res.Failed += r.Failed
		return err
	})
	if err != nil {
		res.Merged = 0
	}
	return res, err
}
