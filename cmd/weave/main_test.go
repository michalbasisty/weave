package main

import (
	"os"
	"strings"
	"testing"
)

// Commands register the project they ran in; keep that out of the real ~/.weave.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "weave-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("WEAVE_HOME", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// `weave --help` is where a user finds the machine-readable reports.
func TestHelpListsCheckFormats(t *testing.T) {
	if !strings.Contains(helpText(), "--format text|markdown|json|sarif") {
		t.Fatal("help does not list the check report formats")
	}
}
