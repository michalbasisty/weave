package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/weave"
)

// The helpers behind `weave spec list|show|save` (and the OpenChamber Specs tab):
// a round trip through a change record, and paths that must stay inside specs/.
func TestSpecHelpersRoundTripAndStayInSpecs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tasks := "# Tasks: a\n\n- [x] T1 One.\n- [ ] T2 Two.\n"
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(tasks), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.md"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}

	g, err := weave.Reindex(root)
	if err != nil {
		t.Fatal(err)
	}
	payload := SpecsPayload(root, g)
	if _, ok := payload["changes"]; !ok {
		t.Fatalf("payload has no changes: %v", payload)
	}

	path := "specs/changes/2026-01-01-a/tasks.md"
	page, err := SpecPage(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if page["text"] != tasks || !strings.Contains(page["html"].(string), "T2 Two.") {
		t.Errorf("page: %v", page)
	}

	if _, err := SaveSpec(root, path, tasks+"- [ ] T3 Three.\n"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "tasks.md")); !strings.Contains(string(got), "T3 Three.") {
		t.Errorf("after save:\n%s", got)
	}

	for _, bad := range []string{"secret.md", "specs/../secret.md", "/etc/passwd", "specs/changes/2026-01-01-a/tasks.txt", "specs/changes/none.md"} {
		if _, err := SpecPage(root, bad); err == nil {
			t.Errorf("SpecPage(%q) read a file outside specs/", bad)
		}
		if _, err := SaveSpec(root, bad, "x"); err == nil {
			t.Errorf("SaveSpec(%q) wrote a file outside specs/", bad)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(root, "secret.md")); string(got) != "no" {
		t.Errorf("secret.md changed: %q", got)
	}
}
