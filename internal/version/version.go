// Package version exposes build-time version information for the miner client.
//
// Version, Commit and Date are injected at build time via:
//
//	-ldflags "-X github.com/Ault-Blockchain/ault-miner-node/internal/version.Version=v1.2.3 ..."
//
// When built without ldflags (e.g. `go install`), the values fall back to the
// Go VCS build stamps embedded by the toolchain.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

var (
	// Version is the release version (git tag), e.g. "v1.1.6".
	Version = "dev"
	// Commit is the full git commit hash.
	Commit = ""
	// Date is the build timestamp in RFC3339 (UTC).
	Date = ""
)

func init() {
	// Injected via ldflags: trust it as-is, do not decorate.
	injected := Version != "dev"

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if Commit == "" {
				Commit = s.Value
			}
		case "vcs.time":
			if Date == "" {
				Date = s.Value
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}

	if injected {
		return
	}

	// Without ldflags, fall back to the module version recorded by `go install`.
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	if dirty && !strings.Contains(Version, "dirty") {
		Version += "-dirty"
	}
}

// ShortCommit returns the abbreviated commit hash.
func ShortCommit() string {
	if len(Commit) > 7 {
		return Commit[:7]
	}
	return Commit
}

// String returns a one-line version summary, e.g.
// "v1.1.6 (a1b2c3d, built 2026-11-11T00:00:00Z)".
func String() string {
	var parts []string
	if c := ShortCommit(); c != "" {
		parts = append(parts, c)
	}
	if Date != "" {
		parts = append(parts, "built "+Date)
	}
	if len(parts) == 0 {
		return Version
	}
	return fmt.Sprintf("%s (%s)", Version, strings.Join(parts, ", "))
}

// Info is the structured version payload served over the API.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Get returns the structured version information.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}
