package main

import (
	"os"
	"path/filepath"
	"testing"
)

// unlockFixture makes a change record with a legacy marker and points root at it.
func unlockFixture(t *testing.T) (full, rel string) {
	t.Helper()
	oldRoot := root
	root = t.TempDir()
	t.Cleanup(func() { root = oldRoot })

	full = filepath.Join(root, "specs", "changes", "2026-01-01-a", "tasks.md")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("# Tasks: a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full+".lock", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return full, "changes/2026-01-01-a/tasks.md"
}

// Without --confirm-stopped nothing is removed and the command fails as a usage
// error, so a script cannot unlock a spec by accident.
func TestSpecUnlockRequiresConfirmation(t *testing.T) {
	full, rel := unlockFixture(t)
	oldArgs := os.Args
	os.Args = []string{"weave", "spec", "unlock", rel}
	t.Cleanup(func() { os.Args = oldArgs })

	if code := specUnlock([]string{rel}); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if _, err := os.Stat(full + ".lock"); err != nil {
		t.Fatalf("marker was removed without confirmation: %v", err)
	}
}

// The flag may be given anywhere on the command line; parseArgs strips it, and the
// direct form is accepted too.
func TestSpecUnlockRemovesMarkerWithConfirmation(t *testing.T) {
	full, rel := unlockFixture(t)
	oldArgs := os.Args
	os.Args = []string{"weave", "spec", "unlock", rel, "--confirm-stopped"}
	t.Cleanup(func() { os.Args = oldArgs })

	if code := specUnlock([]string{rel}); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if _, err := os.Stat(full + ".lock"); !os.IsNotExist(err) {
		t.Errorf("marker still exists: %v", err)
	}
	if _, err := os.Stat(full); err != nil {
		t.Errorf("the spec was touched: %v", err)
	}
}

// A clean spec is reported and succeeds; an unsafe path is a normal failure and
// removes nothing.
func TestSpecUnlockMissingMarkerAndUnsafePath(t *testing.T) {
	full, _ := unlockFixture(t)
	if err := os.Remove(full + ".lock"); err != nil {
		t.Fatal(err)
	}
	if code := specUnlock([]string{"changes/2026-01-01-a/tasks.md", "--confirm-stopped"}); code != 0 {
		t.Errorf("missing marker: exit %d, want 0", code)
	}
	if code := specUnlock([]string{"../outside.md", "--confirm-stopped"}); code != 1 {
		t.Errorf("unsafe path: exit %d, want 1", code)
	}
	if _, err := os.Stat(full); err != nil {
		t.Errorf("the spec disappeared: %v", err)
	}
}

// The argument shape is checked before anything is removed.
func TestSpecUnlockUsageErrors(t *testing.T) {
	_, rel := unlockFixture(t)
	for _, args := range [][]string{
		nil,
		{rel, "second.md", "--confirm-stopped"},
		{rel, "--bogus", "--confirm-stopped"},
	} {
		if code := specUnlock(args); code != 2 {
			t.Errorf("args %v: exit %d, want 2", args, code)
		}
	}
}
