package weave

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Ticks that arrive at once must all survive: the lock is what stops the last
// writer from dropping the others' lines.
func TestConcurrentTicksAllLand(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString("# Tasks: a\n\n")
	const n = 16
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&body, "- [ ] T%d Task %d.\n", i, i)
	}
	path := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := SetSpecTask(root, "2026-01-01-a", "T"+strconv.Itoa(i), true)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("tick: %v", err)
		}
	}
	raw, _ := os.ReadFile(path)
	if got := strings.Count(string(raw), "- [x]"); got != n {
		t.Fatalf("%d of %d ticks landed:\n%s", got, n, raw)
	}
	if _, err := os.Stat(path + ".lock"); err == nil {
		t.Error("the lock outlived the writers")
	}
}

func TestNewAndDeleteSpecChange(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	slug, err := NewSpecChange(root, "billing-retry", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if slug != "2026-11-05-billing-retry" {
		t.Fatalf("slug = %q", slug)
	}
	dir := filepath.Join(root, "specs", "changes", slug)
	for _, f := range []string{"proposal.md", "tasks.md", "spec-delta.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "proposal.md"))
	if !strings.Contains(string(raw), "# Proposal: billing retry") {
		t.Errorf("default title: %s", raw)
	}
	if _, err := NewSpecChange(root, "billing-retry", "", now); err == nil {
		t.Error("a second change with the same slug should fail")
	}
	if _, err := NewSpecChange(root, "../escape", "", now); err == nil {
		t.Error("a name with a separator should fail")
	}
	if _, _, err := SetSpecTask(root, "../escape", "T1", true); err == nil {
		t.Error("a change outside specs/changes should fail")
	}

	if name, err := DeleteSpecChange(root, slug); err != nil || name != slug {
		t.Fatalf("delete: %q %v", name, err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the change directory survived the delete")
	}
	if _, err := DeleteSpecChange(root, slug); err == nil {
		t.Error("deleting a missing change should fail")
	}
}
