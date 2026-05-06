package version

// Version and Commit are set at build time via -ldflags:
//
//	go build -ldflags "-X github.com/huangzheng2016/eTerm/internal/version.Version=v1.0.0 -X github.com/huangzheng2016/eTerm/internal/version.Commit=abc1234"
var (
	Version = "dev"
	Commit  = "unknown"
)
