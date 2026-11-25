package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The demo has to break and then pass on the shop it ships: the archive, the
// edits and the gate all move together.
func TestDemoBreaksAndCatchesARename(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := filepath.Join(t.TempDir(), "shop")
	out, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	if code := runDemo(dir, false, out); code != 0 {
		t.Fatalf("demo exited %d", code)
	}
	_ = out.Close()
	raw, _ := os.ReadFile(out.Name())
	text := string(raw)
	for _, want := range []string{
		"5 screens, 13 endpoints, 5 tables",
		"FAIL removed endpoint still called: GET /api/orders",
		"most likely meant: GET /api/orders/history",
		"Screens /cart and /orders",
		"PASS",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("demo output lacks %q:\n%s", want, text)
		}
	}
}

func TestUnpackDemoWritesEveryFile(t *testing.T) {
	dir := t.TempDir()
	if err := unpackDemo(dir); err != nil {
		t.Fatal(err)
	}
	n := strings.Count(demoArchive, "\n-- ")
	var got int
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			got++
		}
		return nil
	})
	if got != n || got < 20 {
		t.Errorf("unpacked %d files, archive has %d", got, n)
	}
	raw, err := os.ReadFile(filepath.Join(dir, demoRoutes))
	if err != nil || !strings.Contains(string(raw), demoEdits[0].from) {
		t.Errorf("%s lacks the line the demo edits", demoRoutes)
	}
}
