// Package version holds the release version, overridden at build time with
// -ldflags "-X github.com/michalbasisty/weave/internal/version.Version=…".
package version

// Version is the user-visible release string.
var Version = "0.1.0"
