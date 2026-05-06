package version

import "fmt"

const (
	GitHubOwner = "huangzheng2016"
	GitHubRepo  = "eTerm"
)

func ReleaseAssetURL(tag, assetFile string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", GitHubOwner, GitHubRepo, tag, assetFile)
}

const ChecksumsFileName = "SHA256SUMS"
