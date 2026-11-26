package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The default .weave output directory is created through the pinned repository root.
// A link there is refused even when it stays inside the repo, because os.Root cannot
// resolve a junction or an absolute symlink; the error must name the directory and
// the repair rather than only "path escapes from parent".
func TestOpenRepoDirRefusesInRepoWeaveLinkWithRepairHint(t *testing.T) {
	repo := t.TempDir()
	inRepo := filepath.Join(repo, "weave-real")
	if err := os.Mkdir(inRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(repo, ".weave")
	linkDir(t, inRepo, link)

	dir, err := openRepoDir(repo, ".weave")
	if err == nil {
		_ = dir.Close()
		t.Fatal("openRepoDir followed an in-repo .weave link")
	}
	for _, want := range []string{link, "path escapes from parent", "junction", "real directory"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("openRepoDir error %q does not mention %q", err, want)
		}
	}
	entries, readErr := os.ReadDir(inRepo)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Errorf("the linked in-repo directory gained entries: %v", entries)
	}
}

// The same helper still creates a real nested output directory, so the repair is not
// hiding a broken default path.
func TestOpenRepoDirCreatesRealNestedDir(t *testing.T) {
	repo := t.TempDir()
	dir, err := openRepoDir(repo, ".weave/wiki")
	if err != nil {
		t.Fatalf("openRepoDir: %v", err)
	}
	if err := dir.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".weave", "wiki")); err != nil {
		t.Fatalf("the nested output directory was not created: %v", err)
	}
}
