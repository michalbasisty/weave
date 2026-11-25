package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The review ledger never reads or writes through a .weave directory linked out of
// the repo, and the outside ledger keeps its bytes.
func TestReviewLedgerStaysInsideRepo(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	const outsideText = `{"at":"2026-01-01T00:00:00Z","ref":"outside"}` + "\n"
	outsideLog := filepath.Join(outside, "reviews.log")
	if err := os.WriteFile(outsideLog, []byte(outsideText), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, ".weave")
	if err := os.Symlink(outside, link); err != nil {
		if err := junction(outside, link); err != nil {
			t.Skipf("cannot create a symlink or junction here: %v", err)
		}
	}

	if marks := readMarks(root); len(marks) != 0 {
		t.Errorf("marks read through a .weave link: %+v", marks)
	}
	if err := appendMark(root, "inside"); err == nil {
		t.Error("mark appended through a .weave link")
	}
	if raw, _ := os.ReadFile(outsideLog); string(raw) != outsideText {
		t.Errorf("the linked ledger changed: %q", raw)
	}
}

// The ledger is a regular file: a directory in its place is neither read nor
// appended to, and a missing ledger still reads as no marks.
func TestReviewLedgerShape(t *testing.T) {
	root := t.TempDir()
	if marks := readMarks(root); len(marks) != 0 {
		t.Errorf("a missing ledger read as marks: %+v", marks)
	}
	if err := os.MkdirAll(filepath.Join(root, ".weave", "reviews.log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if marks := readMarks(root); len(marks) != 0 {
		t.Errorf("a directory ledger read as marks: %+v", marks)
	}
	if err := appendMark(root, "inside"); err == nil {
		t.Error("appended to a directory ledger")
	}
}

// A writer that died mid-record leaves the ledger's last line without a newline.
// The next mark must not be glued onto it: a separator makes the new mark a record
// of its own, so the newest mark is still what a later review compares against.
func TestReviewLedgerTruncatedFinalLineKeepsNewerMark(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	const oldLine = `{"at":"2026-01-01T00:00:00Z","ref":"old"}` + "\n"
	const truncated = `{"at":"2026-02-02T00:00:00Z","ref":"hal`
	path := filepath.Join(root, ".weave", "reviews.log")
	if err := os.WriteFile(path, []byte(oldLine+truncated), 0o644); err != nil {
		t.Fatal(err)
	}

	marks := readMarks(root)
	if len(marks) != 1 || marks[0].Ref != "old" {
		t.Fatalf("marks before append: %+v", marks)
	}

	if err := appendMark(root, "newer"); err != nil {
		t.Fatal(err)
	}
	marks = readMarks(root)
	if len(marks) != 2 {
		t.Fatalf("marks after append: %+v", marks)
	}
	if got := marks[len(marks)-1].Ref; got != "newer" {
		t.Fatalf("newest mark = %q, want newer", got)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), truncated+"\n") {
		t.Errorf("the truncated record was not kept on its own line: %q", raw)
	}
	if strings.Contains(string(raw), `hal{"at"`) {
		t.Errorf("the new mark was glued to the truncated record: %q", raw)
	}
	if !strings.HasSuffix(string(raw), `"ref":"newer"}`+"\n") {
		t.Errorf("ledger tail = %q", raw)
	}
}

// A ledger longer than the read bound still yields its newest mark: the history is
// read as a bounded tail, never treated as "no marks" because of total length.
func TestReviewLedgerLongHistoryKeepsNewestMark(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	old := `{"at":"2026-01-01T00:00:00Z","ref":"old"}` + "\n"
	for b.Len() < 5<<20 {
		b.WriteString(old)
	}
	// A line far longer than the tail, then the mark a person just left. The tail
	// starts inside that line, so the first partial line must be dropped.
	b.WriteString(strings.Repeat("x", 300<<10))
	b.WriteByte('\n')
	b.WriteString(`{"at":"2026-06-01T00:00:00Z","ref":"recent"}` + "\n")
	path := filepath.Join(root, ".weave", "reviews.log")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	marks := readMarks(root)
	if len(marks) == 0 {
		t.Fatal("a 5 MB ledger read as no marks")
	}
	if got := marks[len(marks)-1].Ref; got != "recent" {
		t.Fatalf("newest mark = %q, want recent", got)
	}
	for _, m := range marks {
		if m.Ref != "old" && m.Ref != "recent" {
			t.Errorf("a partial line was parsed as a mark: %+v", m)
		}
	}

	// The newest append is what a later review compares against.
	if err := appendMark(root, "inside"); err != nil {
		t.Fatal(err)
	}
	marks = readMarks(root)
	if len(marks) == 0 || marks[len(marks)-1].Ref != "inside" {
		t.Fatalf("marks after append: %+v", marks)
	}
}
