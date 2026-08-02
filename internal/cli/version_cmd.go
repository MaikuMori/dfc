package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"
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
	bi, _ := debug.ReadBuildInfo()
	version, commit, date := resolveVersion(Version, Commit, Date, bi)
	return fmt.Sprintf("dfc %s\ncommit: %s\ndate: %s\nruntime: %s %s/%s",
		version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// resolveVersion fills values not injected via ldflags from the embedded
// build info: the module version for `go install` builds, the VCS
// revision/time for source builds.
func resolveVersion(version, commit, date string, bi *debug.BuildInfo) (string, string, string) {
	if bi == nil {
		return version, commit, date
	}
	if version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "none" {
				commit = s.Value
			}
		case "vcs.time":
			if date == "unknown" {
				date = s.Value
			}
		}
	}
	return version, commit, date
}
