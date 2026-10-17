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

// Repository is where rflib's source is published.
const Repository = "https://github.com/rgeyer/rflib"

// Author is who wrote rflib.
const Author = "Ryan Geyer, W6FAM"

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

// SourceURL is where this build's source is: its release's tag, or the
// commit of an unmodified checkout; otherwise the repository, since a
// modified build's changes aren't published there.
func (i Info) SourceURL() string {
	switch {
	case i.Version != "dev":
		return Repository + "/tree/" + i.Version
	case i.Commit != "" && !strings.HasSuffix(i.Commit, "-modified"):
		return Repository + "/tree/" + i.Commit
	}
	return Repository
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
