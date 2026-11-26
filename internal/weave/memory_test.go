package weave

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The same note twice is stored once and shown once.
func TestNotesAreNotRepeated(t *testing.T) {
	root := t.TempDir()
	if n, _ := Remember(root, []string{"a.ts"}, "Invoices are immutable once sent", "2026-01-01T00:00:00.000Z"); n == nil {
		t.Fatal("first note not kept")
	}
	if n, _ := Remember(root, []string{"a.ts"}, "  invoices are  immutable once sent ", ""); n != nil {
		t.Fatal("a repeat was stored")
	}
	if n, _ := Remember(root, []string{"b.ts"}, "Invoices are immutable once sent", "2026-01-02T00:00:00.000Z"); n == nil {
		t.Fatal("the same text on another file is a new note")
	}
	if got := NotesFor(root, []string{"a.ts", "b.ts"}); len(got) != 1 {
		t.Fatalf("context shows %d copies", len(got))
	}
}

// Who owns a file is CODEOWNERS' job: such notes are refused, hidden from context
// and recall, and pruned from the file; a real note survives all three.
func TestOwnershipNotesAreNoise(t *testing.T) {
	for note, want := range map[string]bool{
		"Owner: packages/backend/src/tasks.ts (tasks module).": true,
		"Owners: @ana @ben":         true,
		"owned by the billing team": true,
		"Ownership changed in March because the retry job moved to ops, so ask ops first.": false,
		"Retries are capped at 3: the provider bans above that":                            false,
	} {
		if Derivable(note) != want {
			t.Errorf("Derivable(%q) = %v", note, !want)
		}
	}
	root := t.TempDir()
	if n, _ := Remember(root, []string{"a.ts"}, "Owner: tasks team", ""); n != nil {
		t.Fatal("an ownership note was stored")
	}
	// notes written before this rule, by hand or an older build
	raw := `{"at":"2026-01-01T00:00:00.000Z","files":["a.ts"],"note":"Owner: tasks team"}
{"at":"2026-01-02T00:00:00.000Z","files":["a.ts"],"note":"Owner: tasks team"}
{"at":"2026-01-03T00:00:00.000Z","files":["a.ts"],"note":"Retries are capped at 3: the provider bans above that"}
`
	if err := os.WriteFile(MemoryPath(root), []byte(raw), 0o644); err != nil {
		_ = os.MkdirAll(filepath.Dir(MemoryPath(root)), 0o755)
		if err := os.WriteFile(MemoryPath(root), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := NotesFor(root, []string{"a.ts"}); len(got) != 1 || Derivable(got[0].Note) {
		t.Fatalf("context shows %v", got)
	}
	if n, err := PruneMemory(root); err != nil || n != 2 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	if all, _ := ReadNotes(root); len(all) != 1 {
		t.Fatalf("left %v", all)
	}
}

// Reads leave a missing .weave alone, Remember creates it, and the maintenance
// calls keep their old missing-directory behavior. Every step goes through a pinned
// root, so the created file lives inside the repository.
func TestMemoryMissingDirectoryAndCreate(t *testing.T) {
	root := t.TempDir()
	if notes, err := ReadNotes(root); err != nil || len(notes) != 0 {
		t.Fatalf("ReadNotes without .weave = %v, %v", notes, err)
	}
	if _, err := ClearMemory(root); err == nil {
		t.Fatal("ClearMemory created a memory file in a missing .weave")
	}
	if n, err := PruneMemory(root); err != nil || n != 0 {
		t.Fatalf("PruneMemory without .weave = %d, %v", n, err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".weave")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a read or prune created .weave: %v", err)
	}
	if n, err := Remember(root, []string{"a.ts"}, "Invoices are immutable once sent", "2026-01-01T00:00:00.000Z"); err != nil || n == nil {
		t.Fatalf("Remember did not create .weave: n=%v err=%v", n, err)
	}
	if _, err := os.Stat(MemoryPath(root)); err != nil {
		t.Fatalf("memory file missing after Remember: %v", err)
	}
	if n, err := ClearMemory(root); err != nil || n != 1 {
		t.Fatalf("ClearMemory = %d, %v; want 1 note", n, err)
	}
	if notes, err := ReadNotes(root); err != nil || len(notes) != 0 {
		t.Fatalf("memory after ClearMemory = %v, %v", notes, err)
	}
}

// A .weave directory linked outside the repo must not receive a note, even though
// the duplicate check reads through the same pinned root.
func TestRememberRefusesLinkedWeave(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	dirLink(t, outside, filepath.Join(root, ".weave"))

	n, err := Remember(root, []string{"a.ts"}, "Invoices are immutable once sent", "2026-01-01T00:00:00.000Z")
	if err == nil || n != nil {
		t.Fatalf("Remember through a linked .weave: n=%v err=%v", n, err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "memory.jsonl")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("outside directory gained memory: %v", err)
	}
}

// A final symlink or junction named memory.jsonl must not receive the appended
// note: the pinned root refuses it and the target keeps its bytes.
func TestRememberRefusesFinalLink(t *testing.T) {
	root := t.TempDir()
	weave := filepath.Join(root, ".weave")
	if err := os.MkdirAll(weave, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "memory.jsonl")
	const secretText = `{"at":"2026-01-01T00:00:00.000Z","files":["a.ts"],"note":"outside"}` + "\n"
	if err := os.WriteFile(secret, []byte(secretText), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(weave, "memory.jsonl")
	if err := os.Symlink(secret, link); err == nil {
		n, err := Remember(root, []string{"a.ts"}, "Invoices are immutable once sent", "2026-01-02T00:00:00.000Z")
		if err == nil || n != nil {
			t.Fatalf("Remember through a final link: n=%v err=%v", n, err)
		}
		if raw, _ := os.ReadFile(secret); string(raw) != secretText {
			t.Errorf("the link's target changed: %q", raw)
		}
		return
	} else if runtime.GOOS != "windows" {
		t.Fatalf("create a file symlink, which this Unix test requires: %v", err)
	}
	// Windows without the symlink privilege: a junction at the same name stands in.
	dirLink(t, outside, link)
	n, err := Remember(root, []string{"a.ts"}, "Invoices are immutable once sent", "2026-01-02T00:00:00.000Z")
	if err == nil || n != nil {
		t.Fatalf("Remember through a final junction: n=%v err=%v", n, err)
	}
	if raw, _ := os.ReadFile(secret); string(raw) != secretText {
		t.Errorf("the junction's target changed: %q", raw)
	}
}
