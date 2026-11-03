package weave

import (
	"os"
	"path/filepath"
	"testing"
)

// A task title wraps at the file's column width. Everything on the following lines
// belongs to the title except a labelled evidence line, which is evidence.
func TestTaskTitleKeepsItsWrappedLines(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-wrapped")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Tasks\n\n- [x] T1 Short `code`,\n      the rest of the title.\n      Evidence: `a/b.ts`.\n"
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	g := BuildGraph(root)
	var got string
	for _, n := range g.Nodes {
		if n.Kind == KindTask {
			got = metaText(n, "title")
		}
	}
	const want = "Short `code`, the rest of the title."
	if got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}
