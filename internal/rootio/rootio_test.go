package rootio

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// inRepoLink points link at target, both inside the test root. os.Symlink needs a
// privilege on Windows that a checkout usually lacks, so a directory junction stands
// in there. Both forms carry an absolute target, which os.Root does not resolve.
func inRepoLink(t *testing.T, target, link string) {
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

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot(%q): %v", dir, err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("close root: %v", err)
		}
	})
	return r
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(b)
}

func TestOpenRegularRejectsTrunc(t *testing.T) {
	dir := t.TempDir()
	r := openRoot(t, dir)
	target := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, flags := range []int{
		os.O_WRONLY | os.O_TRUNC,
		os.O_RDWR | os.O_TRUNC,
		os.O_WRONLY | os.O_CREATE | os.O_TRUNC,
	} {
		f, err := OpenRegular(r, "keep.txt", flags, 0o600)
		if f != nil {
			_ = f.Close()
			t.Fatalf("flags %#x: unexpected handle", flags)
		}
		if !errors.Is(err, ErrTruncate) {
			t.Fatalf("flags %#x: err = %v, want ErrTruncate", flags, err)
		}
		if got := readFile(t, target); got != "keep" {
			t.Fatalf("flags %#x: file changed to %q", flags, got)
		}
	}

	if _, err := OpenRegular(r, "new.txt", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600); !errors.Is(err, ErrTruncate) {
		t.Fatalf("missing target with O_TRUNC: err = %v, want ErrTruncate", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "new.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("O_TRUNC created a file: %v", err)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	r := openRoot(t, dir)

	if err := WriteFile(r, "a.txt", []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile create: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "hello" {
		t.Fatalf("after create: got %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("perm = %o, want 600", perm)
		}
	}

	if err := WriteFile(r, "a.txt", []byte("bye"), 0o600); err != nil {
		t.Fatalf("WriteFile replace: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "bye" {
		t.Fatalf("after shorter replace: got %q, want no leftover", got)
	}

	if err := WriteFile(r, "a.txt", nil, 0o600); err != nil {
		t.Fatalf("WriteFile truncate to empty: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "" {
		t.Fatalf("after empty write: got %q", got)
	}

	if err := WriteFile(r, "missing/a.txt", []byte("x"), 0o600); err == nil {
		t.Fatal("WriteFile created a missing parent directory")
	}
	if _, err := os.Lstat(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("parent directory exists: %v", err)
	}
}

func TestAppendFile(t *testing.T) {
	dir := t.TempDir()
	r := openRoot(t, dir)

	if err := AppendFile(r, "log.txt", []byte("one"), 0o600); err != nil {
		t.Fatalf("AppendFile create: %v", err)
	}
	if err := AppendFile(r, "log.txt", []byte("two"), 0o600); err != nil {
		t.Fatalf("AppendFile append: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "log.txt")); got != "onetwo" {
		t.Fatalf("got %q, want onetwo", got)
	}

	if err := WriteFile(r, "log.txt", []byte("X"), 0o600); err != nil {
		t.Fatalf("WriteFile replace: %v", err)
	}
	if err := AppendFile(r, "log.txt", []byte("Y"), 0o600); err != nil {
		t.Fatalf("AppendFile after replace: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "log.txt")); got != "XY" {
		t.Fatalf("got %q, want XY", got)
	}

	if err := AppendFile(r, "missing/log.txt", []byte("x"), 0o600); err == nil {
		t.Fatal("AppendFile created a missing parent directory")
	}
	if _, err := os.Lstat(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("parent directory exists: %v", err)
	}
}

func TestOpenRegularWrites(t *testing.T) {
	dir := t.TempDir()
	r := openRoot(t, dir)

	f, err := OpenRegular(r, "b.txt", os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("OpenRegular: %v", err)
	}
	if _, err := f.WriteString("via handle"); err != nil {
		_ = f.Close()
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "b.txt")); got != "via handle" {
		t.Fatalf("got %q", got)
	}

	if _, err := OpenRegular(r, "b.txt", os.O_WRONLY|os.O_TRUNC, 0o600); !errors.Is(err, ErrTruncate) {
		t.Fatalf("err = %v, want ErrTruncate", err)
	}
	if got := readFile(t, filepath.Join(dir, "b.txt")); got != "via handle" {
		t.Fatalf("file changed to %q", got)
	}
}

func TestOpenRegularRejectsNonRegular(t *testing.T) {
	dir := t.TempDir()
	r := openRoot(t, dir)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	f, err := OpenRegular(r, "sub", os.O_RDONLY, 0)
	if f != nil {
		_ = f.Close()
		t.Fatal("opened a directory")
	}
	if err == nil {
		t.Fatal("directory accepted")
	}
	if runtime.GOOS != "windows" && !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}

	if _, err := OpenRegular(r, "absent.txt", os.O_RDONLY, 0); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: err = %v, want ErrNotExist", err)
	}
}

func TestOutsideLinksRefused(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	r := openRoot(t, rootDir)
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("absolute file link", func(t *testing.T) {
		link := filepath.Join(rootDir, "link.txt")
		if err := os.Symlink(outsideFile, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := WriteFile(r, "link.txt", []byte("pwned"), 0o600); err == nil {
			t.Fatal("WriteFile followed a link outside the root")
		}
		if err := AppendFile(r, "link.txt", []byte("pwned"), 0o600); err == nil {
			t.Fatal("AppendFile followed a link outside the root")
		}
		if got := readFile(t, outsideFile); got != "secret" {
			t.Fatalf("outside file changed to %q", got)
		}
	})

	t.Run("relative escaping link", func(t *testing.T) {
		rel, err := filepath.Rel(rootDir, outsideFile)
		if err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(rootDir, "rel-link.txt")
		if err := os.Symlink(rel, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := WriteFile(r, "rel-link.txt", []byte("pwned"), 0o600); err == nil {
			t.Fatal("WriteFile followed an escaping relative link")
		}
		if got := readFile(t, outsideFile); got != "secret" {
			t.Fatalf("outside file changed to %q", got)
		}
	})

	t.Run("directory link", func(t *testing.T) {
		link := filepath.Join(rootDir, "linkdir")
		if err := os.Symlink(outsideDir, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := WriteFile(r, "linkdir/new.txt", []byte("pwned"), 0o600); err == nil {
			t.Fatal("WriteFile followed a directory link outside the root")
		}
		if _, err := os.Lstat(filepath.Join(outsideDir, "new.txt")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("outside directory gained a file: %v", err)
		}
	})

	t.Run("dotdot", func(t *testing.T) {
		if err := WriteFile(r, "../escape.txt", []byte("pwned"), 0o600); err == nil {
			t.Fatal("WriteFile accepted a path above the root")
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(rootDir), "escape.txt")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("file written above the root: %v", err)
		}
	})
}

func TestRootPinning(t *testing.T) {
	parent := t.TempDir()
	rootDir := filepath.Join(parent, "repo")
	if err := os.Mkdir(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if err := WriteFile(r, "first.txt", []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(parent, "moved")
	if err := os.Rename(rootDir, moved); err != nil {
		t.Skipf("cannot rename an open root on this platform: %v", err)
	}
	if err := os.Mkdir(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := WriteFile(r, "second.txt", []byte("two"), 0o600); err != nil {
		t.Fatalf("write through pinned root: %v", err)
	}
	if got := readFile(t, filepath.Join(moved, "second.txt")); got != "two" {
		t.Fatalf("pinned root wrote %q into the moved directory", got)
	}
	if _, err := os.Lstat(filepath.Join(rootDir, "second.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("write landed in the replacement directory: %v", err)
	}
}

// A link that stays inside the root is still refused when its target is absolute:
// os.Root reports "path escapes from parent", which describes the unresolved target
// rather than a real escape. The helpers must keep that refusal and add the repair.
func TestInRepoLinkRefusedWithActionableError(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	inRepoLink(t, real, link)

	r := openRoot(t, dir)
	err := WriteFile(r, filepath.Join("link", "new.txt"), []byte("x"), 0o600)
	if err == nil {
		t.Fatal("WriteFile followed a link that stays inside the root")
	}
	if !PathEscapes(err) {
		t.Fatalf("refusal is not reported as a root escape: %v", err)
	}
	if got, statErr := os.Lstat(filepath.Join(real, "new.txt")); statErr == nil {
		t.Fatalf("the link's in-repo target received the write: %v", got)
	}

	wrapped := ExplainPathEscapes(link, err)
	for _, want := range []string{link, "junction", "absolute symlink", "real directory"} {
		if !strings.Contains(wrapped.Error(), want) {
			t.Errorf("error %q does not mention %q", wrapped, want)
		}
	}
	if !errors.Is(wrapped, err) {
		t.Errorf("the repair dropped the original refusal: %v", wrapped)
	}
	if PathEscapes(nil) {
		t.Error("PathEscapes(nil) = true")
	}
}

// os.Root follows a relative link whose target stays inside the root; only an
// absolute target (and hence a Windows junction) is refused.
func TestRelativeInRootSymlinkFollowed(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "rel")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	r := openRoot(t, dir)
	if err := WriteFile(r, filepath.Join("rel", "out.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile through a relative in-root link: %v", err)
	}
	if got := readFile(t, filepath.Join(real, "out.txt")); got != "x" {
		t.Fatalf("got %q through the relative link", got)
	}
}

// ExplainPathEscapes leaves an error that is not os.Root's refusal untouched.
func TestExplainPathEscapesLeavesOtherErrorsAlone(t *testing.T) {
	plain := errors.New("plain failure")
	if PathEscapes(plain) {
		t.Fatal("PathEscapes claimed a plain error")
	}
	if got := ExplainPathEscapes("x", plain); got != plain {
		t.Fatalf("an unrelated error was rewritten: %v", got)
	}
}
