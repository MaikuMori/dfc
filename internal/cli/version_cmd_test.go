package cli

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	t.Parallel()

	moduleInstall := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.5.0"},
	}
	sourceBuild := &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.time", Value: "2026-08-02T12:00:00Z"},
		},
	}

	tests := []struct {
		name                              string
		version, commit, date             string
		bi                                *debug.BuildInfo
		wantVersion, wantCommit, wantDate string
	}{
		{
			name:    "no build info keeps defaults",
			version: "dev", commit: "none", date: "unknown",
			bi:          nil,
			wantVersion: "dev", wantCommit: "none", wantDate: "unknown",
		},
		{
			name:    "go install fills module version",
			version: "dev", commit: "none", date: "unknown",
			bi:          moduleInstall,
			wantVersion: "v0.5.0", wantCommit: "none", wantDate: "unknown",
		},
		{
			name:    "source build fills vcs revision and time",
			version: "dev", commit: "none", date: "unknown",
			bi:          sourceBuild,
			wantVersion: "dev", wantCommit: "abc123", wantDate: "2026-08-02T12:00:00Z",
		},
		{
			name:    "ldflags values win over build info",
			version: "v0.6.0", commit: "def456", date: "2026-09-01T00:00:00Z",
			bi:          sourceBuild,
			wantVersion: "v0.6.0", wantCommit: "def456", wantDate: "2026-09-01T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			version, commit, date := resolveVersion(tt.version, tt.commit, tt.date, tt.bi)
			if version != tt.wantVersion || commit != tt.wantCommit || date != tt.wantDate {
				t.Errorf("resolveVersion() = (%q, %q, %q), want (%q, %q, %q)",
					version, commit, date, tt.wantVersion, tt.wantCommit, tt.wantDate)
			}
		})
	}
}
