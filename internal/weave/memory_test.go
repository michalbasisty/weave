package weave

import (
	"os"
	"path/filepath"
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
