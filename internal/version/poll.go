package version

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
)

const (
	LastSuccessfulUpdateCheckKey = "last_successful_update_check_at"
	UpdateCheckMinInterval       = 6 * time.Hour
)

// PollLatestRelease queries GitHub when allowed: not disabled, DB throttle elapsed, env clear.
// On HTTP 200 and successful JSON decode, records the check time in AppSetting (even if already up to date).
func PollLatestRelease(gdb *gorm.DB, disabled bool) (tag, url string, err error) {
	if disabled {
		return "", "", nil
	}
	if gdb != nil {
		if s, e := db.GetSetting(gdb, LastSuccessfulUpdateCheckKey); e == nil && s != "" {
			t, perr := time.Parse(time.RFC3339Nano, s)
			if perr != nil {
				_ = db.SetSetting(gdb, LastSuccessfulUpdateCheckKey, "")
			} else if time.Since(t) < UpdateCheckMinInterval {
				return "", "", nil
			}
		}
	}

	tag, url, ok, err := fetchLatestRelease()
	if err != nil {
		return "", "", err
	}
	if ok && gdb != nil {
		_ = db.SetSetting(gdb, LastSuccessfulUpdateCheckKey, time.Now().UTC().Format(time.RFC3339Nano))
	}
	return tag, url, nil
}

func fetchLatestRelease() (tag, url string, responseOK bool, err error) {
	client := updateHTTPClient(5 * time.Second)
	resp, err := client.Get("https://api.github.com/repos/huangzheng2016/eTerm/releases/latest")
	if err != nil {
		return "", "", false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", false, nil
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", false, err
	}
	responseOK = true
	if rel.TagName == "" {
		return "", "", responseOK, nil
	}
	if isNewer(rel.TagName, Version) {
		return rel.TagName, rel.HTMLURL, responseOK, nil
	}
	return "", "", responseOK, nil
}
