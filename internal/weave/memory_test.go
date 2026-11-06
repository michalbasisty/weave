package weave

import "testing"

// The same note twice is stored once and shown once.
func TestNotesAreNotRepeated(t *testing.T) {
	root := t.TempDir()
	if n, _ := Remember(root, []string{"a.ts"}, "Owner: billing team", "2026-01-01T00:00:00.000Z"); n == nil {
		t.Fatal("first note not kept")
	}
	if n, _ := Remember(root, []string{"a.ts"}, "  owner:  billing team ", ""); n != nil {
		t.Fatal("a repeat was stored")
	}
	if n, _ := Remember(root, []string{"b.ts"}, "Owner: billing team", "2026-01-02T00:00:00.000Z"); n == nil {
		t.Fatal("the same text on another file is a new note")
	}
	if got := NotesFor(root, []string{"a.ts", "b.ts"}); len(got) != 1 {
		t.Fatalf("context shows %d copies", len(got))
	}
}
