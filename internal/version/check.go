package version

import (
	"strings"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func CheckLatestRelease() (tag, url string, err error) {
	t, u, _, err := fetchLatestRelease()
	return t, u, err
}

func isNewer(remote, local string) bool {
	if local == "dev" || local == "" {
		return false
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

func normalizeSemver(s string) []int {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
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
