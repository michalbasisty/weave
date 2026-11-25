package weave

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specFor makes a spec file and, when lock is true, its legacy marker. It returns
// the repo-relative spec path with slashes.
func specFor(t *testing.T, rel string, lock bool) string {
	t.Helper()
	root := t.TempDir()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("# spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if lock {
		if err := os.WriteFile(full+".lock", nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The legacy marker is removed through the confined specs handle, and the spec
// itself is untouched. Both the spec-relative and repo-relative spellings work.
func TestUnlockSpecLockRemovesRegularMarker(t *testing.T) {
	for _, rel := range []string{"specs/changes/2026-01-01-a/tasks.md", "changes/2026-01-01-a/tasks.md"} {
		root := specFor(t, "specs/changes/2026-01-01-a/tasks.md", true)
		full := filepath.Join(root, "specs", "changes", "2026-01-01-a", "tasks.md")
		got, err := UnlockSpecLock(root, rel)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if want := "specs/changes/2026-01-01-a/tasks.md.lock"; got != want {
			t.Errorf("%s: marker path %q, want %q", rel, got, want)
		}
		if _, err := os.Stat(full + ".lock"); !os.IsNotExist(err) {
			t.Errorf("%s: marker still exists: %v", rel, err)
		}
		if raw, err := os.ReadFile(full); err != nil || string(raw) != "# spec\n" {
			t.Errorf("%s: the spec was touched: %q %v", rel, raw, err)
		}
	}
}

// A marker directly under specs works too: the directory root is specs itself.
func TestUnlockSpecLockAtSpecsRoot(t *testing.T) {
	root := specFor(t, "specs/overview.md", true)
	if _, err := UnlockSpecLock(root, "overview.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "specs", "overview.md.lock")); !os.IsNotExist(err) {
		t.Errorf("marker still exists: %v", err)
	}
}

// An already-clean spec is not a failure: the caller reports nothing to remove.
func TestUnlockSpecLockMissingMarker(t *testing.T) {
	root := specFor(t, "specs/changes/2026-01-01-a/tasks.md", false)
	got, err := UnlockSpecLock(root, "changes/2026-01-01-a/tasks.md")
	if !errors.Is(err, ErrNoLockMarker) {
		t.Fatalf("err = %v, want ErrNoLockMarker", err)
	}
	if want := "specs/changes/2026-01-01-a/tasks.md.lock"; got != want {
		t.Errorf("marker path %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "specs", "changes", "2026-01-01-a", "tasks.md")); err != nil {
		t.Errorf("the spec disappeared: %v", err)
	}
}

// A link at the marker name is not the legacy marker: refusing it must not remove
// the link's target, inside or outside the repo.
func TestUnlockSpecLockRefusesSymlinkMarker(t *testing.T) {
	root := specFor(t, "specs/changes/2026-01-01-a/tasks.md", false)
	outside := filepath.Join(filepath.Dir(root), "outside-unlock-target")
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	marker := filepath.Join(dir, "tasks.md.lock")
	if err := os.Symlink(outside, marker); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	if _, err := UnlockSpecLock(root, "changes/2026-01-01-a/tasks.md"); err == nil {
		t.Fatal("a symlink marker was removed")
	} else if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("error does not say why: %v", err)
	}
	if _, err := os.Lstat(marker); err != nil {
		t.Errorf("the marker link was removed: %v", err)
	}
}

// A directory (or any non-regular name) at the marker path is refused untouched.
func TestUnlockSpecLockRefusesNonRegularMarker(t *testing.T) {
	root := specFor(t, "specs/changes/2026-01-01-a/tasks.md", false)
	marker := filepath.Join(root, "specs", "changes", "2026-01-01-a", "tasks.md.lock")
	if err := os.Mkdir(marker, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := UnlockSpecLock(root, "changes/2026-01-01-a/tasks.md"); err == nil {
		t.Fatal("a directory marker was removed")
	}
	if info, err := os.Stat(marker); err != nil || !info.IsDir() {
		t.Errorf("the directory marker changed: %v %v", info, err)
	}
}

// Paths that leave specs, are absolute, or are not Markdown are refused before
// any handle is used.
func TestUnlockSpecLockUnsafePaths(t *testing.T) {
	root := specFor(t, "specs/changes/2026-01-01-a/tasks.md", true)
	outside := filepath.Join(root, "outside.md")
	if err := os.WriteFile(outside+".lock", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(root, "specs", "changes", "2026-01-01-a", "tasks.md")
	for _, rel := range []string{
		"", " ", "../outside.md", "specs/../outside.md", "../../outside.md",
		"changes/2026-01-01-a/tasks.txt", "changes/2026-01-01-a", "specs//changes/x.md",
		abs, "/outside.md",
	} {
		if _, err := UnlockSpecLock(root, rel); !errors.Is(err, ErrSpecTarget) {
			t.Errorf("%q: err = %v, want ErrSpecTarget", rel, err)
		}
	}
	for _, p := range []string{
		filepath.Join(root, "outside.md.lock"),
		filepath.Join(root, "specs", "changes", "2026-01-01-a", "tasks.md.lock"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("refused path removed %s: %v", p, err)
		}
	}
}
