package weave

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// dirLink points link at target. A symlink is used where the OS allows it; on
// Windows, where os.Symlink needs a privilege, a directory junction stands in. A
// Unix test that cannot make its symlink fails instead of skipping: the link is the
// thing under test there, and a silent skip would hide lost coverage.
func dirLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	} else if runtime.GOOS != "windows" {
		t.Fatalf("create a symlink, which this Unix test requires: %v", err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction: %v: %s", err, strings.TrimSpace(string(out)))
	}
}

// A parent directory renamed and replaced by a link after it was pinned must not
// receive a redirected write: the os.Root handle still names the original
// directory, and the link's target is untouched. This is the race the old
// EvalSymlinks-then-open sequence lost.
func TestSaveSpecTextSurvivesParentSwap(t *testing.T) {
	root := t.TempDir()
	rel := "specs/changes/2026-01-01-a/tasks.md"
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "tasks.md"), []byte("# outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	parent, base, err := openSpecDir(root, rel)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()

	moved := dir + ".moved"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatalf("swap the pinned directory: %v", err)
	}
	dirLink(t, outside, dir)

	if err := saveFileAt(parent, base, "# pinned\n"); err != nil {
		t.Fatalf("save through the pinned parent: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(outside, "tasks.md")); string(raw) != "# outside\n" {
		t.Errorf("the swapped link received the write: %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(moved, "tasks.md")); string(raw) != "# pinned\n" {
		t.Errorf("the pinned directory did not receive the write: %q", raw)
	}
	if entries, _ := os.ReadDir(moved); len(entries) != 1 {
		t.Errorf("temp or lock files survived in the pinned directory: %v", entries)
	}
}

// A final symlink or junction is not a spec file: the editor refuses it rather
// than renaming over it, and the link's target keeps its bytes. A Windows machine
// without the symlink privilege gets the junction form.
func TestSaveSpecTextRefusesFinalLink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.md")
	const secretText = "# outside\n"
	if err := os.WriteFile(secret, []byte(secretText), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "specs", "link.md")
	if err := os.Symlink(secret, link); err == nil {
		if err := SaveSpecText(root, "specs/link.md", "# pwned\n"); !errors.Is(err, ErrSpecTarget) {
			t.Fatalf("save over a final link: %v, want ErrSpecTarget", err)
		}
		if raw, _ := os.ReadFile(secret); string(raw) != secretText {
			t.Errorf("the link's target changed: %q", raw)
		}
		return
	} else if runtime.GOOS != "windows" {
		t.Fatalf("create a file symlink, which this Unix test requires: %v", err)
	}
	keep := filepath.Join(outside, "keep.md")
	if err := os.WriteFile(keep, []byte(secretText), 0o644); err != nil {
		t.Fatal(err)
	}
	dirLink(t, outside, link)
	if err := SaveSpecText(root, "specs/link.md", "# pwned\n"); !errors.Is(err, ErrSpecTarget) {
		t.Fatalf("save over a final junction: %v, want ErrSpecTarget", err)
	}
	if raw, err := os.ReadFile(keep); err != nil || string(raw) != secretText {
		t.Errorf("the junction's target changed: %q %v", raw, err)
	}
}

// A directory named like a spec is not a file the editor may replace.
func TestSaveSpecTextRefusesNonRegularTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs", "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveSpecText(root, "specs/dir.md", "# pwned\n"); !errors.Is(err, ErrSpecTarget) {
		t.Fatalf("save over a directory: %v, want ErrSpecTarget", err)
	}
}

// The repo Root refuses a directory swapped for a link out of the repo, whether
// the swap happened before the call or between opening the Root and reading.
func TestReadRepoFileRefusesSwappedParent(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.ts"), []byte("export const b = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "b.ts"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pinned, err := openRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()

	if err := os.Rename(sub, sub+".moved"); err != nil {
		t.Fatalf("swap the directory: %v", err)
	}
	dirLink(t, outside, sub)

	if raw, err := readFileAt(pinned, "sub/b.ts", 1024); err == nil {
		t.Fatalf("read through the swapped directory with a pinned root: %q", raw)
	}
	if raw, err := ReadRepoFile(root, "sub/b.ts", 1024); err == nil {
		t.Fatalf("read through the swapped directory: %q", raw)
	}
}

// The specs directory itself cannot be swapped for a link outside the repo: both
// the renderer and the editor refuse, and the outside file keeps its bytes.
func TestSpecTextRefusesSpecsDirectorySwap(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "capabilities"), 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "capabilities", "x.md")
	if err := os.WriteFile(secret, []byte("# outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirLink(t, outside, filepath.Join(root, "specs"))

	if _, err := ReadSpecText(root, "specs/capabilities/x.md", 512*1024); err == nil {
		t.Fatal("read through a specs directory link")
	}
	if err := SaveSpecText(root, "specs/capabilities/x.md", "# pwned\n"); err == nil {
		t.Fatal("save through a specs directory link")
	}
	if raw, _ := os.ReadFile(secret); string(raw) != "# outside\n" {
		t.Errorf("the linked specs directory received the write: %q", raw)
	}
}

// A change directory linked out of specs/changes cannot be ticked, deleted or
// recreated; the outside directory survives intact.
func TestSpecWritersRefuseLinkedChangeDir(t *testing.T) {
	root := t.TempDir()
	changes := filepath.Join(root, "specs", "changes")
	if err := os.MkdirAll(changes, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideTasks := filepath.Join(outside, "tasks.md")
	const outsideText = "# Tasks: outside\n\n- [ ] T1 Outside.\n"
	if err := os.WriteFile(outsideTasks, []byte(outsideText), 0o644); err != nil {
		t.Fatal(err)
	}
	dirLink(t, outside, filepath.Join(changes, "2026-01-01-leak"))

	if _, _, err := SetSpecTask(root, "2026-01-01-leak", "T1", true); err == nil {
		t.Error("ticked a task through a link out of specs")
	}
	if raw, _ := os.ReadFile(outsideTasks); string(raw) != outsideText {
		t.Errorf("the linked tasks file changed: %q", raw)
	}
	if _, err := DeleteSpecChange(root, "2026-01-01-leak"); err == nil {
		t.Error("deleted a change through a link out of specs")
	}
	if _, err := os.Stat(outsideTasks); err != nil {
		t.Errorf("delete removed the outside directory's contents: %v", err)
	}
	if _, err := NewSpecChange(root, "leak", "", time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)); err == nil {
		t.Error("created a change over a link out of specs")
	}
}

// Two editors saving at once through the pinned directory: the lock serializes
// them, the result is one whole document, and no temp or lock file is left.
func TestConcurrentSpecSavesLeaveOneWholeDocument(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "specs", "spec.md")
	if err := os.WriteFile(path, []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const n = 8
	texts := make([]string, n)
	for i := range texts {
		texts[i] = "# Spec " + strings.Repeat(string(rune('a'+i)), 4096) + "\n"
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- SaveSpecText(root, "specs/spec.md", texts[i])
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	whole := false
	for _, text := range texts {
		if string(raw) == text {
			whole = true
		}
	}
	if !whole {
		t.Errorf("the file is not one of the writers' documents: %d bytes", len(raw))
	}
	for _, name := range []string{"spec.md.lock"} {
		if _, err := os.Stat(filepath.Join(root, "specs", name)); err == nil {
			t.Errorf("%s survived the writers", name)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(root, "specs"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasSuffix(e.Name(), ".lock") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

// A change is created once: a leaf that already exists is never reused or
// overwritten, and a leaf that is a link out of specs is refused with its target
// untouched. This covers the check-then-create window the old Stat/MkdirAll pair
// left open.
func TestNewSpecChangeNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	slug, err := NewSpecChange(root, "billing-retry", "", now)
	if err != nil {
		t.Fatal(err)
	}
	proposal := filepath.Join(root, "specs", "changes", slug, "proposal.md")
	if err := os.WriteFile(proposal, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSpecChange(root, "billing-retry", "", now); err == nil {
		t.Fatal("created a second record with the same slug")
	}
	if raw, _ := os.ReadFile(proposal); string(raw) != "# mine\n" {
		t.Errorf("an existing record was overwritten: %q", raw)
	}

	outside := t.TempDir()
	outsideProposal := filepath.Join(outside, "proposal.md")
	const outsideText = "# outside\n"
	if err := os.WriteFile(outsideProposal, []byte(outsideText), 0o644); err != nil {
		t.Fatal(err)
	}
	dirLink(t, outside, filepath.Join(root, "specs", "changes", "2026-01-02-leak"))
	if _, err := NewSpecChange(root, "leak", "", time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("created a record over a link out of specs")
	}
	if raw, _ := os.ReadFile(outsideProposal); string(raw) != outsideText {
		t.Errorf("the linked leaf received a file: %q", raw)
	}
}

// A change record that is a link is not deleted, even when it points at another
// record inside specs: the target survives.
func TestDeleteSpecChangeRefusesLinkedRecord(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "tasks.md"), []byte("# Tasks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirLink(t, target, filepath.Join(root, "specs", "changes", "2026-01-01-b"))

	if _, err := DeleteSpecChange(root, "2026-01-01-b"); err == nil {
		t.Fatal("deleted a change through a link")
	}
	if _, err := os.Stat(filepath.Join(target, "tasks.md")); err != nil {
		t.Errorf("the link's target was deleted: %v", err)
	}
}

// tasks.md must be a regular file: a directory or link in its place is refused, not
// replaced, and an outside target keeps its bytes.
func TestSetSpecTaskRefusesNonRegularTasksFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	if err := os.MkdirAll(filepath.Join(dir, "tasks.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SetSpecTask(root, "2026-01-01-a", "T1", true); err == nil {
		t.Error("ticked a task whose tasks.md is a directory")
	}

	other := filepath.Join(root, "specs", "changes", "2026-01-01-b")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "tasks.md")
	const outsideText = "# Tasks: outside\n\n- [ ] T1 Outside.\n"
	if err := os.WriteFile(outside, []byte(outsideText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(other, "tasks.md")); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatalf("create a file symlink, which this Unix test requires: %v", err)
		}
		return // no file-symlink privilege on Windows; the directory case above still covers shape
	}
	if _, _, err := SetSpecTask(root, "2026-01-01-b", "T1", true); err == nil {
		t.Error("ticked a task through a final link")
	}
	if raw, _ := os.ReadFile(outside); string(raw) != outsideText {
		t.Errorf("the link's target changed: %q", raw)
	}
}

// A linked changes directory is a traversal failure, not "no change": the error
// must name the read that failed.
func TestChangeResolutionReportsTraversalErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirLink(t, t.TempDir(), filepath.Join(root, "specs", "changes"))

	_, _, err := SetSpecTask(root, "2026-01-01-x", "T1", true)
	if err == nil {
		t.Fatal("ticked a task through a linked changes directory")
	}
	if !strings.Contains(err.Error(), "cannot read") {
		t.Errorf("the traversal failure is hidden: %v", err)
	}
	if _, err := DeleteSpecChange(root, "2026-01-01-x"); err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Errorf("delete error: %v", err)
	}
}

// No lock is ever removed on another writer's behalf: an abandoned lock and a live,
// paused writer look the same from outside, and stealing the live one would drop its
// update. The save fails after the deadline, names the lock file, and leaves both the
// target and the lock untouched.
func TestHeldLockIsNeverStolen(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "specs", "spec.md")
	const oldText = "# old\n"
	if err := os.WriteFile(path, []byte(oldText), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := path + ".lock"

	for _, age := range []time.Duration{0, time.Minute} {
		if err := os.WriteFile(lock, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if age > 0 {
			old := time.Now().Add(-age)
			if err := os.Chtimes(lock, old, old); err != nil {
				t.Fatal(err)
			}
		}
		err := SaveSpecText(root, "specs/spec.md", "# new\n")
		if err == nil {
			t.Fatalf("a %v old lock was stolen", age)
		}
		if !strings.Contains(err.Error(), "locked by another writer") || !strings.Contains(err.Error(), "remove that lock file") {
			t.Errorf("the lock error is not actionable: %v", err)
		}
		if !strings.Contains(err.Error(), lock) {
			t.Errorf("the lock error does not name %s: %v", lock, err)
		}
		if raw, _ := os.ReadFile(path); string(raw) != oldText {
			t.Errorf("a save under a %v old lock changed the file: %q", age, raw)
		}
		if _, statErr := os.Stat(lock); statErr != nil {
			t.Errorf("the %v old lock was removed: %v", age, statErr)
		}
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
	}

	// After the manual cleanup the same save succeeds.
	if err := SaveSpecText(root, "specs/spec.md", "# new\n"); err != nil {
		t.Fatalf("save after manual cleanup: %v", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "# new\n" {
		t.Errorf("file after cleanup and save: %q", raw)
	}
	if _, err := os.Stat(lock); err == nil {
		t.Error("the lock outlived the successful writer")
	}
}

// AppendRepoFile creates its parent and appends through the pinned repository root.
func TestAppendRepoFileAppends(t *testing.T) {
	root := t.TempDir()
	if err := AppendRepoFile(root, ".weave/reviews.log", []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendRepoFile(root, ".weave/reviews.log", []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadRepoFile(root, ".weave/reviews.log", 1024)
	if err != nil || string(raw) != "a\nb\n" {
		t.Fatalf("ledger: %q %v", raw, err)
	}
}

// The confined helpers take repo-relative slash paths only: an absolute path in
// either the platform's form or the POSIX root-relative form, a drive-relative
// path and a NUL byte are refused before any handle is opened.
func TestRepoRelRefusesAbsoluteAndNUL(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "x.ts")
	refused := []string{abs, "/etc/passwd", "a\x00b"}
	if os.PathSeparator == '\\' {
		refused = append(refused, `C:\Windows\win.ini`, `C:relative.ts`, `\\server\share\x.ts`)
	}
	for _, rel := range refused {
		if name, err := repoRel(rel); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("repoRel(%q) = %q, %v; want ErrOutsideRoot", rel, name, err)
		}
	}
	if name, err := repoRel("./sub/a.ts"); err != nil || name != filepath.Join("sub", "a.ts") {
		t.Errorf("repoRel(./sub/a.ts) = %q, %v", name, err)
	}
}

// Absolute and NUL paths are refused by every confined reader and writer, so an
// existing file outside the repo keeps its bytes and no handle reaches it.
func TestConfinedIORejectsAbsoluteAndNULPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	const secretText = "keep\n"
	if err := os.WriteFile(secret, []byte(secretText), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{secret, "/etc/passwd", "bad\x00name"} {
		if raw, err := ReadRepoFile(root, rel, 1024); err == nil {
			t.Errorf("ReadRepoFile(%q) = %q", rel, raw)
		}
		if raw, err := ReadRepoFileTail(root, rel, 1024); err == nil {
			t.Errorf("ReadRepoFileTail(%q) = %q", rel, raw)
		}
		if err := AppendRepoFile(root, rel, []byte("pwned\n"), 0o644); err == nil {
			t.Errorf("AppendRepoFile(%q) accepted a refused path", rel)
		}
		if err := AppendRepoLine(root, rel, []byte("pwned\n"), 0o644); err == nil {
			t.Errorf("AppendRepoLine(%q) accepted a refused path", rel)
		}
	}
	if raw, _ := os.ReadFile(secret); string(raw) != secretText {
		t.Errorf("the outside file changed: %q", raw)
	}
}

// AppendRepoLine starts a new line when the file does not end one, and adds no
// blank line when it does. A record a dead writer left unterminated stays intact
// and cannot absorb the next one.
func TestAppendRepoLineSeparatesTruncatedRecord(t *testing.T) {
	root := t.TempDir()
	const rel = ".weave/reviews.log"
	const oldLine = `{"ref":"old"}` + "\n"
	const truncated = `{"ref":"half`
	if err := AppendRepoLine(root, rel, []byte(oldLine), 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulate a writer that died mid-record: no trailing newline.
	if err := AppendRepoFile(root, rel, []byte(truncated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendRepoLine(root, rel, []byte(`{"ref":"new"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadRepoFile(root, rel, 4096)
	if err != nil {
		t.Fatal(err)
	}
	want := oldLine + truncated + "\n" + `{"ref":"new"}` + "\n"
	if string(raw) != want {
		t.Fatalf("ledger = %q, want %q", raw, want)
	}

	// A terminated record gets the new line directly after it.
	if err := AppendRepoLine(root, rel, []byte(`{"ref":"next"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ = ReadRepoFile(root, rel, 4096)
	if !strings.HasSuffix(string(raw), `{"ref":"new"}`+"\n"+`{"ref":"next"}`+"\n") {
		t.Fatalf("a blank separator was added after a terminated line: %q", raw)
	}
}

// The separator check reads and writes through the pinned repository root: a
// .weave directory linked out of the repo receives neither.
func TestAppendRepoLineRefusesLinkedWeaveDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	const outsideText = "outside\n"
	outsideLog := filepath.Join(outside, "reviews.log")
	if err := os.WriteFile(outsideLog, []byte(outsideText), 0o644); err != nil {
		t.Fatal(err)
	}
	dirLink(t, outside, filepath.Join(root, ".weave"))

	if err := AppendRepoLine(root, ".weave/reviews.log", []byte("pwned\n"), 0o644); err == nil {
		t.Error("appended a line through a linked .weave directory")
	}
	if raw, _ := os.ReadFile(outsideLog); string(raw) != outsideText {
		t.Errorf("the linked ledger changed: %q", raw)
	}
}

// A .weave directory swapped for a link out of the repo cannot receive an append,
// whether the swap is already there or happens after the repo root was pinned.
func TestAppendRepoFileRefusesLinkedWeaveDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	const outsideText = "outside\n"
	outsideLog := filepath.Join(outside, "reviews.log")
	if err := os.WriteFile(outsideLog, []byte(outsideText), 0o644); err != nil {
		t.Fatal(err)
	}
	dirLink(t, outside, filepath.Join(root, ".weave"))

	if _, err := ReadRepoFile(root, ".weave/reviews.log", 1024); err == nil {
		t.Error("read through a linked .weave directory")
	}
	if err := AppendRepoFile(root, ".weave/reviews.log", []byte("pwned\n"), 0o644); err == nil {
		t.Error("appended through a linked .weave directory")
	}
	if raw, _ := os.ReadFile(outsideLog); string(raw) != outsideText {
		t.Errorf("the linked ledger changed: %q", raw)
	}

	root2 := t.TempDir()
	realWeave := filepath.Join(root2, ".weave")
	if err := os.MkdirAll(realWeave, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realWeave, "reviews.log"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside2 := t.TempDir()
	outsideLog2 := filepath.Join(outside2, "reviews.log")
	if err := os.WriteFile(outsideLog2, []byte(outsideText), 0o644); err != nil {
		t.Fatal(err)
	}
	pinned, err := openRoot(root2)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	if err := os.Rename(realWeave, realWeave+".moved"); err != nil {
		t.Fatalf("swap the pinned directory: %v", err)
	}
	dirLink(t, outside2, realWeave)

	if err := appendFileAt(pinned, filepath.Join(".weave", "reviews.log"), []byte("pwned\n"), 0o644); err == nil {
		t.Error("appended through a .weave swap under a pinned root")
	}
	if raw, _ := os.ReadFile(outsideLog2); string(raw) != outsideText {
		t.Errorf("the swapped link received the append: %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(realWeave+".moved", "reviews.log")); string(raw) != "old\n" {
		t.Errorf("the pinned directory changed: %q", raw)
	}
}

// A tail read keeps the newest complete lines of a long file in bounded memory. The
// first, possibly cut, line is dropped; a tail with no line break yields nothing
// rather than a fragment.
func TestReadRepoFileTailKeepsRecentLines(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".weave", "reviews.log")
	var b strings.Builder
	oldLine := `{"at":"2026-01-01T00:00:00Z","ref":"old"}` + "\n"
	for b.Len() < 1<<20+2048 {
		b.WriteString(oldLine)
	}
	b.WriteString(`{"at":"2026-02-02T00:00:00Z","ref":"recent"}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	const tailMax = 4096
	raw, err := ReadRepoFileTail(root, ".weave/reviews.log", tailMax)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > tailMax {
		t.Fatalf("tail read %d bytes, want at most %d", len(raw), tailMax)
	}
	if !strings.Contains(string(raw), `"ref":"recent"`) {
		t.Fatalf("the newest line is not in the tail: %q", raw)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("a partial line survived the tail cut: %q", line)
		}
	}

	// A file under the bound is read whole.
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err = ReadRepoFileTail(root, ".weave/reviews.log", tailMax)
	if err != nil || string(raw) != "one\ntwo\n" {
		t.Fatalf("short file: %q %v", raw, err)
	}

	// A tail without a line break yields nothing, not a fragment.
	if err := os.WriteFile(path, []byte(strings.Repeat("x", tailMax*2)), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err = ReadRepoFileTail(root, ".weave/reviews.log", tailMax)
	if err != nil || len(raw) != 0 {
		t.Fatalf("unbroken line: %q %v", raw, err)
	}

	// The confined handle still refuses a linked directory.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "reviews.log"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root2 := t.TempDir()
	dirLink(t, outside, filepath.Join(root2, ".weave"))
	if _, err := ReadRepoFileTail(root2, ".weave/reviews.log", tailMax); err == nil {
		t.Error("read a tail through a linked .weave directory")
	}
}
