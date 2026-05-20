package cli

import (
	"fmt"
	"runtime"
)

// Populated at build time via -ldflags by GoReleaser.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// VersionCmd prints build information.
type VersionCmd struct{}

func (VersionCmd) Run() error {
	fmt.Println(VersionString())
	return nil
}

func VersionString() string {
	return fmt.Sprintf("dfc %s\ncommit: %s\ndate: %s\nruntime: %s %s/%s",
		Version, Commit, Date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
