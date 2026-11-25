//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package weave

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests use syscall.Mkfifo, which the Go syscall package exposes on Linux,
// Darwin, the BSDs and their derivatives (Android and iOS included). AIX, Solaris,
// illumos and Hurd lack it, so the file does not build there.

// fifoWatchdog bounds every confined call made against a FIFO. A correct
// implementation returns at once; a blocking open never returns, and the watchdog
// turns that hang into a named test failure instead of a stuck test binary.
const fifoWatchdog = 10 * time.Second

// noBlockValue runs f and fails the test if it is still running at the deadline.
func noBlockValue[T any](t *testing.T, what string, f func() (T, error)) (T, error) {
	t.Helper()
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := f()
		done <- result{v, err}
	}()
	select {
	case r := <-done:
		return r.v, r.err
	case <-time.After(fifoWatchdog):
		t.Fatalf("%s did not return within %v: a FIFO target blocked it", what, fifoWatchdog)
		var zero T
		return zero, nil
	}
}

// noBlock is noBlockValue for a call with no useful result.
func noBlock(t *testing.T, what string, f func() error) error {
	t.Helper()
	_, err := noBlockValue(t, what, func() (struct{}, error) {
		return struct{}{}, f()
	})
	return err
}

// assertPerm checks a path's permission bits.
func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}

// A FIFO planted at a read, size-check or append target must be refused, not
// waited on. Opening a FIFO for reading waits for a writer and opening it for
// writing waits for a reader; the non-blocking open plus the handle's regular-file
// check make every confined call return at once.
func TestUnixContainmentFIFODoesNotBlock(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, "specs")
	for _, dir := range []string{specs, filepath.Join(root, ".weave"), filepath.Join(specs, "changes", "2026-01-01-a")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pipe := filepath.Join(root, "pipe")
	ledger := filepath.Join(root, ".weave", "reviews.log")
	specPipe := filepath.Join(specs, "pipe.md")
	for _, path := range []string{pipe, ledger, specPipe} {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatalf("mkfifo %s: %v", path, err)
		}
	}

	if raw, err := noBlockValue(t, "ReadRepoFile on a FIFO", func() ([]byte, error) {
		return ReadRepoFile(root, "pipe", 1024)
	}); !errors.Is(err, ErrUnreadable) {
		t.Errorf("ReadRepoFile on a FIFO = %q, %v; want ErrUnreadable", raw, err)
	}
	if raw, err := noBlockValue(t, "ReadRepoFileTail on a FIFO", func() ([]byte, error) {
		return ReadRepoFileTail(root, ".weave/reviews.log", 1024)
	}); !errors.Is(err, ErrUnreadable) {
		t.Errorf("ReadRepoFileTail on a FIFO = %q, %v; want ErrUnreadable", raw, err)
	}
	if err := noBlock(t, "AppendRepoFile on a FIFO", func() error {
		return AppendRepoFile(root, "pipe", []byte("pwned\n"), 0o644)
	}); err == nil {
		t.Error("AppendRepoFile accepted a FIFO target")
	}
	if err := noBlock(t, "AppendRepoLine on a FIFO", func() error {
		return AppendRepoLine(root, ".weave/reviews.log", []byte("pwned\n"), 0o644)
	}); err == nil {
		t.Error("AppendRepoLine accepted a FIFO target")
	}

	if err := noBlock(t, "ReadSpecText on a FIFO", func() error {
		_, err := ReadSpecText(root, "specs/pipe.md", 1024)
		return err
	}); !errors.Is(err, ErrUnreadable) {
		t.Errorf("ReadSpecText on a FIFO: %v; want ErrUnreadable", err)
	}
	if err := noBlock(t, "SpecFileSize on a FIFO", func() error {
		_, err := SpecFileSize(root, "specs/pipe.md")
		return err
	}); !errors.Is(err, ErrUnreadable) {
		t.Errorf("SpecFileSize on a FIFO: %v; want ErrUnreadable", err)
	}
	if err := noBlock(t, "SaveSpecText over a FIFO", func() error {
		return SaveSpecText(root, "specs/pipe.md", "# pwned\n")
	}); !errors.Is(err, ErrSpecTarget) {
		t.Errorf("SaveSpecText over a FIFO: %v; want ErrSpecTarget", err)
	}

	// The refused calls must leave the FIFOs as FIFOs: no byte was appended and
	// no path was replaced by a regular file.
	for _, path := range []string{pipe, ledger, specPipe} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("stat %s after the refused calls: %v", path, err)
		}
		if info.Mode()&os.ModeNamedPipe == 0 {
			t.Errorf("%s is no longer a FIFO: %v", path, info.Mode())
		}
	}
}

// A save or a task replace keeps the target's own permission bits. The temp file
// that carries the new text starts private (no group or other access) and is
// chmodded to the target's mode before the rename, so the mode survives the atomic
// replace instead of being reset to 0644.
func TestUnixContainmentPreservesPermissionBits(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(root, "specs", "spec.md")
	if err := os.WriteFile(spec, []byte("# old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(spec, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := SaveSpecText(root, "specs/spec.md", "# new\n"); err != nil {
		t.Fatalf("save: %v", err)
	}
	assertPerm(t, spec, 0o750)
	if raw, err := os.ReadFile(spec); err != nil || string(raw) != "# new\n" {
		t.Fatalf("saved spec = %q, %v", raw, err)
	}

	tasks := filepath.Join(dir, "tasks.md")
	const tasksText = "# Tasks\n\n- [ ] T1 First.\n"
	if err := os.WriteFile(tasks, []byte(tasksText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tasks, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := SetSpecTask(root, "2026-01-01-a", "T1", true); err != nil || !changed {
		t.Fatalf("tick: changed=%v err=%v", changed, err)
	}
	assertPerm(t, tasks, 0o640)
	if raw, err := os.ReadFile(tasks); err != nil || !strings.Contains(string(raw), "- [x] T1 First.") {
		t.Fatalf("tasks after tick = %q, %v", raw, err)
	}

	// The temp file is created owner-only: a partial document is never readable by
	// the group or others, even before the mode is copied from the target.
	parent, err := openRoot(filepath.Join(root, "specs"))
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	name, tmp, err := newTempAt(parent, "spec.md")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = tmp.Close()
		_ = parent.Remove(name)
	}()
	info, err := tmp.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("a new temp file is not private: mode %o", info.Mode().Perm())
	}
}

// A FIFO named like a source file must not block the walk: os.ReadFile would wait
// for a writer, while the non-blocking open plus the handle's regular-file check
// skip it. A real file beside it is still read, and the handle's size check refuses
// a file that grew past the walk's cap.
func TestUnixContainmentWalkSkipsFIFO(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.ts")
	const body = "export const a = 1;\n"
	if err := os.WriteFile(real, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "pipe.ts"), filepath.Join(root, "specs", "pipe.md")} {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatalf("mkfifo %s: %v", path, err)
		}
	}

	files, err := noBlockValue(t, "Walk over FIFO entries", func() ([]SourceFile, error) {
		return Walk(root), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Rel != "real.ts" || files[0].Text != body {
		t.Fatalf("Walk over FIFO entries = %v; want only real.ts with its text", files)
	}
	if _, err := readSourceFile(real, 8); !errors.Is(err, ErrUnreadable) {
		t.Errorf("readSourceFile over its cap: %v; want ErrUnreadable", err)
	}

	// The refused walk leaves the FIFOs as FIFOs.
	for _, path := range []string{filepath.Join(root, "pipe.ts"), filepath.Join(root, "specs", "pipe.md")} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("stat %s after the walk: %v", path, err)
		}
		if info.Mode()&os.ModeNamedPipe == 0 {
			t.Errorf("%s is no longer a FIFO: %v", path, info.Mode())
		}
	}
}

// A FIFO named tasks.md must fail the tick at once instead of waiting for a writer,
// and it must leave the FIFO and no lock or temp file behind.
func TestUnixContainmentSetSpecTaskFIFODoesNotBlock(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tasks := filepath.Join(dir, "tasks.md")
	if err := syscall.Mkfifo(tasks, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	var err error
	noBlock(t, "SetSpecTask on a FIFO tasks.md", func() error {
		_, _, err = SetSpecTask(root, "2026-01-01-a", "T1", true)
		return err
	})
	if !errors.Is(err, ErrUnreadable) {
		t.Errorf("SetSpecTask on a FIFO = %v; want ErrUnreadable", err)
	}
	info, statErr := os.Lstat(tasks)
	if statErr != nil {
		t.Fatalf("stat tasks.md after the tick: %v", statErr)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("tasks.md is no longer a FIFO: %v", info.Mode())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "tasks.md" {
			t.Errorf("the refused tick left %s", e.Name())
		}
	}
}

// The tick's text comes from the one handle that passed the regular-file check: a
// different regular file swapped under the same name after the check cannot change
// what is read. A final link is still refused, even a relative in-root link os.Root
// could follow, and its target keeps its bytes.
func TestUnixContainmentTaskReadUsesCheckedHandle(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tasks := filepath.Join(dir, "tasks.md")
	const original = "# Tasks\n\n- [ ] T1 First.\n"
	if err := os.WriteFile(tasks, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	pinned, name, err := changeRoot(root, "2026-01-01-a")
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	f, err := openCheckedAt(pinned, "tasks.md")
	if err != nil {
		t.Fatalf("openCheckedAt: %v", err)
	}
	if name != "2026-01-01-a" {
		t.Fatalf("changeRoot name = %q", name)
	}
	// Swap in a different regular file after the handle was checked.
	if err := os.Rename(tasks, tasks+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tasks, []byte("# swapped\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil || string(raw) != original {
		t.Fatalf("read after a name swap = %q, %v; want the checked file's text", raw, err)
	}

	// A relative in-root link is not the record either: the tick refuses it before
	// trusting any content, and the link's target is untouched.
	linkDir := filepath.Join(root, "specs", "changes", "2026-01-01-b")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(linkDir, "real.md")
	const targetText = "# Tasks\n\n- [ ] T1 Linked.\n"
	if err := os.WriteFile(target, []byte(targetText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.md", filepath.Join(linkDir, "tasks.md")); err != nil {
		t.Fatalf("create a relative symlink: %v", err)
	}
	if _, _, err := SetSpecTask(root, "2026-01-01-b", "T1", true); !errors.Is(err, ErrUnreadable) {
		t.Errorf("tick through an in-root relative link = %v; want ErrUnreadable", err)
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) != targetText {
		t.Errorf("the link's target changed: %q, %v", raw, err)
	}
}

// os.Root follows a relative symlink that stays inside the pinned root and refuses
// an absolute target, even one that names a file inside that root. The tests pin
// that difference: an absolute link is the stricter refusal the package documents,
// and a relative in-root link keeps working. A save target is still never a link of
// either form.
func TestUnixContainmentSymlinkRelativeInsideRootVsAbsolute(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, "specs")
	if err := os.MkdirAll(specs, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(specs, "real.md")
	if err := os.WriteFile(real, []byte("# real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join(specs, "rel.md")
	if err := os.Symlink("real.md", rel); err != nil {
		t.Fatalf("create a relative symlink: %v", err)
	}
	abs := filepath.Join(specs, "abs.md")
	if err := os.Symlink(real, abs); err != nil {
		t.Fatalf("create an absolute symlink: %v", err)
	}

	if got, err := ReadSpecText(root, "specs/rel.md", 1024); err != nil || got != "# real\n" {
		t.Errorf("read through an in-root relative link = %q, %v; want the target's text", got, err)
	}
	if got, err := ReadRepoFile(root, "specs/rel.md", 1024); err != nil || string(got) != "# real\n" {
		t.Errorf("repo read through an in-root relative link = %q, %v", got, err)
	}
	if got, err := ReadSpecText(root, "specs/abs.md", 1024); err == nil {
		t.Errorf("read through an absolute link inside specs = %q; want a refusal", got)
	}
	if got, err := ReadRepoFile(root, "specs/abs.md", 1024); err == nil {
		t.Errorf("repo read through an absolute link = %q; want a refusal", got)
	}

	// Neither link is a file the save route may replace, and the real file keeps
	// its bytes.
	for _, relPath := range []string{"specs/rel.md", "specs/abs.md"} {
		if err := SaveSpecText(root, relPath, "# pwned\n"); !errors.Is(err, ErrSpecTarget) {
			t.Errorf("SaveSpecText(%s) = %v; want ErrSpecTarget", relPath, err)
		}
	}
	if raw, err := os.ReadFile(real); err != nil || string(raw) != "# real\n" {
		t.Errorf("the link's target changed: %q, %v", raw, err)
	}
}
