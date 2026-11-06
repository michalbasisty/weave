// Package version holds the release version, overridden at build time with
// -ldflags "-X github.com/michalbasisty/weave/internal/version.Version=…".
// A plain `go install …@v0.2.0` sets no ldflags, so the module version Go
// recorded in the binary is used instead.
package version

import (
	"runtime/debug"
	"strings"
)

// Version is the user-visible release string.
var Version = "dev"

func init() {
	if Version != "dev" {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := fromModule(bi.Main.Version); v != "" {
			Version = v
		}
	}
}

// fromModule turns a module version ("v0.2.0") into a release string ("0.2.0").
// "(devel)" and an empty version mean a local build and give "".
func fromModule(v string) string {
	if v == "" || v == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}
