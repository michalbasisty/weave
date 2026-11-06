package weave

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A gitignored folder is build output: the walk must not read it as source.
func TestWalkSkipsGitIgnored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "src/generated/\n*.gen.ts\n")
	write("src/app.ts", "export const a = 1\n")
	write("src/generated/prisma/models/User.ts", "export type User = {}\n")
	write("src/schema.gen.ts", "export const s = 1\n")
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Skip(string(out))
	}
	var got []string
	for _, f := range Walk(root) {
		got = append(got, f.Rel)
	}
	if len(got) != 1 || got[0] != "src/app.ts" {
		t.Fatalf("walk read %v", got)
	}
	if n := len(WalkInfo(root)); n != 1 {
		t.Fatalf("WalkInfo listed %d files", n)
	}
}
