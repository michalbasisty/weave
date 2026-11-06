package weave

import (
	"os"
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
