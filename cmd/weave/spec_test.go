package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A tick and a reopen change one checkbox each and leave the other line alone.
func TestSpecTaskSetChangesOneBox(t *testing.T) {
	old := root
	root = t.TempDir()
	defer func() { root = old }()

	path := filepath.Join(root, "specs", "changes", "2026-01-01-a", "tasks.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Tasks: a\n\n- [ ] T1 One. Evidence: `a.ts`\n- [x] T2 Two.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := specTaskSet([]string{"2026-01-01-a", "T1"}, true); code != 0 {
		t.Fatalf("done: exit %d", code)
	}
	got, _ := os.ReadFile(path)
	if want := "- [x] T1 One. Evidence: `a.ts`\n- [x] T2 Two."; !strings.Contains(string(got), want) {
		t.Errorf("after done:\n%s", got)
	}

	if code := specTaskSet([]string{"2026-01-01-a", "T2"}, false); code != 0 {
		t.Fatalf("reopen: exit %d", code)
	}
	got, _ = os.ReadFile(path)
	if !strings.Contains(string(got), "- [ ] T2 Two.") {
		t.Errorf("after reopen:\n%s", got)
	}

	// an unknown task fails instead of rewriting the file
	if code := specTaskSet([]string{"2026-01-01-a", "T9"}, true); code != 1 {
		t.Errorf("unknown task: exit %d, want 1", code)
	}
}
