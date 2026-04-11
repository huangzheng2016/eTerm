package version

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckLatestRelease queries GitHub for the latest release of eTerm.
// Returns the tag and URL if a newer version is available, or empty strings if up-to-date.
// Errors are returned but callers should treat them as non-fatal.
func CheckLatestRelease() (tag, url string, err error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/huangzheng2016/eTerm/releases/latest")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", nil // no release yet or rate-limited
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}

	if rel.TagName == "" {
		return "", "", nil
	}

	if isNewer(rel.TagName, Version) {
		return rel.TagName, rel.HTMLURL, nil
	}
	return "", "", nil
}

// isNewer reports whether remote version is newer than local.
// Compares semver-style: v1.2.3 > v1.2.2. Falls back to string comparison.
func isNewer(remote, local string) bool {
	if local == "dev" || local == "" {
		return false // dev builds don't show update notices
	}
	rv := normalizeSemver(remote)
	lv := normalizeSemver(local)
	if len(rv) == 3 && len(lv) == 3 {
		for i := 0; i < 3; i++ {
			if rv[i] > lv[i] {
				return true
			}
			if rv[i] < lv[i] {
				return false
			}
		}
		return false
	}
	return remote > local
}

// normalizeSemver parses "v1.2.3" into [1, 2, 3].
func normalizeSemver(s string) []int {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		// Strip pre-release suffix (e.g., "3-beta")
		if dash := strings.IndexByte(p, '-'); dash >= 0 {
			p = p[:dash]
		}
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return nil
			}
			n = n*10 + int(c-'0')
		}
		nums[i] = n
	}
	return nums
}
