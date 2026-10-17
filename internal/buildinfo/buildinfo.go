// Package buildinfo says which rflib this is. A release build sets Version
// with -ldflags "-X github.com/ryan/rflib/internal/buildinfo.Version=v0.1.0";
// any other build is "dev", with the commit Go recorded when it was built
// from a git checkout.
package buildinfo

import (
	"runtime/debug"
	"strings"
	"time"
)

// Version is the release, such as v0.1.0, or "dev".
var Version = "dev"

// Info is a build's version, commit, and commit time.
type Info struct {
	Version string `json:"version"`
	// Commit is the git commit, with "-modified" when the checkout had
	// uncommitted changes; "" when unknown.
	Commit string    `json:"commit,omitempty"`
	Time   time.Time `json:"time,omitzero"`
	Go     string    `json:"go"`
}

// Get returns this build's Info.
func Get() Info {
	info := Info{Version: Version}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	info.Go = bi.GoVersion
	modified := false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Commit = s.Value
		case "vcs.time":
			info.Time, _ = time.Parse(time.RFC3339, s.Value)
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if modified && info.Commit != "" {
		info.Commit += "-modified"
	}
	return info
}

// String is the version and, when known, the short commit: "v0.1.0
// (1a2b3c4d)".
func (i Info) String() string {
	if i.Commit == "" {
		return i.Version
	}
	c, modified := strings.CutSuffix(i.Commit, "-modified")
	if len(c) > 8 {
		c = c[:8]
	}
	if modified {
		c += "-modified"
	}
	return i.Version + " (" + c + ")"
}
