package weave

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Reading and writing inside the repository goes through os.Root. A Root pins a
// directory handle, and every later operation resolves inside that handle: a
// component swapped for a symlink or junction after a check cannot redirect the
// operation. That replaces the resolved-once checks the panel used to make, which
// lost the race between EvalSymlinks and the open that followed it.
//
// A Root follows a relative link that stays inside it and refuses one that leaves.
// Two refusals are stricter than the old resolved-path check, and deliberate:
// os.Root does not accept an absolute link target, and on Windows it refuses every
// junction and other reparse point (a junction's target is absolute) even when that
// target is inside the root. A repo that relies on an absolute in-repo link under
// specs/ or in a file path is therefore refused, not followed.

var (
	// ErrOutsideRoot refuses a path that leaves the pinned directory.
	ErrOutsideRoot = errors.New("outside the repo")
	// ErrUnreadable is the panel's message for a missing, irregular or overlarge file.
	ErrUnreadable = errors.New("unreadable or larger than 512 KB")
	// ErrSpecTarget marks a spec the editor must not replace: missing, not a regular
	// file, or reached through a final symlink. The route answers 400 for it.
	ErrSpecTarget = errors.New("spec file cannot be edited")
)

// openRoot pins dir. The path itself is resolved once, by OpenRoot; everything
// after that is relative to the handle, never to the path string.
func openRoot(dir string) (*os.Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return os.OpenRoot(abs)
}

// openSpecsRoot pins <root>/specs through the repository root, so the specs
// directory itself cannot be swapped for a link out of the repo before it is used.
func openSpecsRoot(root string) (*os.Root, error) {
	repo, err := openRoot(root)
	if err != nil {
		return nil, err
	}
	defer repo.Close()
	return repo.OpenRoot("specs")
}

// specSub checks the shape a spec path must have and returns it relative to
// specs/. The check is defence in depth: os.Root, not this function, is what keeps
// the read inside specs. A path outside specs, with a separator escape or without
// the .md suffix is refused.
func specSub(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if !strings.HasPrefix(rel, "specs/") || !strings.HasSuffix(rel, ".md") || strings.Contains(rel, "..") || strings.Contains(rel, `\`) {
		return "", ErrSpecTarget
	}
	sub := strings.TrimPrefix(rel, "specs/")
	if sub == "" || strings.HasPrefix(sub, "/") || strings.HasSuffix(sub, "/") || strings.Contains(sub, "//") {
		return "", ErrSpecTarget
	}
	return sub, nil
}

// specDirSub is specSub's directory form: the shape check for a listing under
// specs/. os.Root is what keeps the listing inside specs.
func specDirSub(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if !strings.HasPrefix(rel, "specs/") || strings.Contains(rel, "..") || strings.Contains(rel, `\`) {
		return "", ErrSpecTarget
	}
	sub := strings.TrimSuffix(strings.TrimPrefix(rel, "specs/"), "/")
	if sub == "" || strings.HasPrefix(sub, "/") || strings.Contains(sub, "//") {
		return "", ErrSpecTarget
	}
	return sub, nil
}

// SpecSubdirs lists the real directory names directly under one directory in specs,
// through the pinned specs root. An entry that is a link is not listed: os.Root
// refuses an absolute target (and every Windows junction) even when it points
// inside the repo, and the panel reads only the record trees it pins.
func SpecSubdirs(root, rel string) ([]string, error) {
	sub, err := specDirSub(rel)
	if err != nil {
		return nil, err
	}
	specs, err := openSpecsRoot(root)
	if err != nil {
		return nil, err
	}
	defer specs.Close()
	entries, err := fs.ReadDir(specs.FS(), sub)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		info, err := specs.Lstat(path.Join(sub, e.Name()))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

// readFileAt opens name inside r, checks the handle, and reads at most max bytes
// from that same handle. A file swapped for another file or a link after the open
// cannot change what is read, and growth during the read cannot outrun the limit.
func readFileAt(r *os.Root, name string, max int64) ([]byte, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, ErrUnreadable
	}
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > max {
		return nil, ErrUnreadable
	}
	return raw, nil
}

// readFileTailAt opens name inside r and returns at most max bytes from its end,
// through the same handle. When the file is longer than max the read starts at a
// line boundary: the first, possibly partial, line is dropped so every returned line
// is complete. Reading a tail keeps a long append-only file useful without holding
// all of it in memory, and a file swapped after the open cannot change the result.
func readFileTailAt(r *os.Root, name string, max int64) ([]byte, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrUnreadable
	}
	size := info.Size()
	if size <= max {
		raw, err := io.ReadAll(io.LimitReader(f, max+1))
		if err != nil {
			return nil, err
		}
		if int64(len(raw)) > max {
			return nil, ErrUnreadable
		}
		return raw, nil
	}
	if _, err := f.Seek(size-max, io.SeekStart); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return nil, err
	}
	if i := bytes.IndexByte(raw, '\n'); i >= 0 {
		return raw[i+1:], nil
	}
	return nil, nil
}

// repoRel turns a slash path inside the repository into the name os.Root takes,
// refusing an absolute path, a drive-relative path or a NUL byte. It does not
// resolve "..": os.Root does, and refuses the ones that leave the root.
func repoRel(rel string) (string, error) {
	name := filepath.FromSlash(strings.TrimPrefix(rel, "./"))
	// A leading separator is an absolute path on the platform's own form: "/x" on
	// Unix and "\x" on Windows. filepath.IsAbs does not call the Windows form
	// absolute, so check the separator too; the API only takes repo-relative paths.
	if name == "" || strings.ContainsRune(name, 0) || filepath.IsAbs(name) ||
		filepath.VolumeName(name) != "" || strings.HasPrefix(name, string(filepath.Separator)) {
		return "", ErrOutsideRoot
	}
	return name, nil
}

// ReadRepoFile reads at most max bytes of rel inside root. The browser's window
// view and the spec renderer use it; neither reopens the path after the check.
func ReadRepoFile(root, rel string, max int64) ([]byte, error) {
	name, err := repoRel(rel)
	if err != nil {
		return nil, err
	}
	r, err := openRoot(root)
	if err != nil {
		return nil, ErrUnreadable
	}
	defer r.Close()
	raw, err := readFileAt(r, name, max)
	if err != nil {
		return nil, ErrUnreadable
	}
	return raw, nil
}

// ReadRepoFileTail reads at most max bytes from the end of rel inside root, through
// the pinned repository root and one handle. It returns complete trailing lines, so
// an append-only ledger of any length still yields its recent records in bounded
// memory. A missing file is an error, as it is for ReadRepoFile.
func ReadRepoFileTail(root, rel string, max int64) ([]byte, error) {
	name, err := repoRel(rel)
	if err != nil {
		return nil, err
	}
	r, err := openRoot(root)
	if err != nil {
		return nil, ErrUnreadable
	}
	defer r.Close()
	raw, err := readFileTailAt(r, name, max)
	if err != nil {
		return nil, ErrUnreadable
	}
	return raw, nil
}

// AppendRepoFile appends b to rel inside root, creating the parent directory, all
// through the pinned repository root. A .weave directory or log file swapped for a
// link out of the repo is refused instead of followed. It is the review ledger's
// only writer.
func AppendRepoFile(root, rel string, b []byte, perm fs.FileMode) error {
	name, err := repoRel(rel)
	if err != nil {
		return err
	}
	r, err := openRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	return appendFileAt(r, name, b, perm)
}

// appendFileAt creates the parent of name and appends b, inside the pinned root.
func appendFileAt(r *os.Root, name string, b []byte, perm fs.FileMode) error {
	if dir := filepath.Dir(name); dir != "." && dir != string(os.PathSeparator) {
		if err := r.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := r.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// AppendRepoLine appends one line record to rel inside root, through the pinned
// repository root like AppendRepoFile. It first makes sure the file ends a line: a
// writer that died mid-record leaves a final line without a newline, and a plain
// append would glue the new record onto it, where neither line parses. One handle
// checks the last byte and writes the separator and the record, so the repair and
// the append cannot be redirected by a swapped path.
func AppendRepoLine(root, rel string, b []byte, perm fs.FileMode) error {
	name, err := repoRel(rel)
	if err != nil {
		return err
	}
	r, err := openRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	return appendLineAt(r, name, b, perm)
}

// appendLineAt appends b to name inside the pinned root, writing a separator first
// when the existing last byte is not a newline. Two writers racing here can leave
// an extra blank line, which a line reader skips, but they cannot glue two records
// onto one line.
func appendLineAt(r *os.Root, name string, b []byte, perm fs.FileMode) error {
	if dir := filepath.Dir(name); dir != "." && dir != string(os.PathSeparator) {
		if err := r.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := r.OpenFile(name, os.O_RDWR|os.O_APPEND|os.O_CREATE, perm)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	if size := info.Size(); size > 0 {
		if _, err := f.Seek(size-1, io.SeekStart); err != nil {
			_ = f.Close()
			return err
		}
		var last [1]byte
		if _, err := io.ReadFull(f, last[:]); err != nil {
			_ = f.Close()
			return err
		}
		if last[0] != '\n' {
			if _, err := f.Write([]byte("\n")); err != nil {
				_ = f.Close()
				return err
			}
		}
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ReadSpecText reads one Markdown file under specs/, contained by a Root pinned at
// the specs directory, and never by a pathname resolved a second time.
func ReadSpecText(root, rel string, max int64) (string, error) {
	sub, err := specSub(rel)
	if err != nil {
		return "", err
	}
	specs, err := openSpecsRoot(root)
	if err != nil {
		return "", ErrUnreadable
	}
	defer specs.Close()
	raw, err := readFileAt(specs, sub, max)
	if err != nil {
		return "", ErrUnreadable
	}
	return string(raw), nil
}

// SpecFileSize reports the size of an existing spec file, from the handle it
// opened. The save route asks first, so a missing or overlarge target is still a
// 400 rather than a write error.
func SpecFileSize(root, rel string) (int64, error) {
	sub, err := specSub(rel)
	if err != nil {
		return 0, err
	}
	specs, err := openSpecsRoot(root)
	if err != nil {
		return 0, ErrUnreadable
	}
	defer specs.Close()
	f, err := specs.Open(sub)
	if err != nil {
		return 0, ErrUnreadable
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, ErrUnreadable
	}
	return info.Size(), nil
}

// openSpecDir pins the directory that holds one spec file and returns its base
// name. Both the lock and the replacement then happen through that handle.
func openSpecDir(root, rel string) (*os.Root, string, error) {
	sub, err := specSub(rel)
	if err != nil {
		return nil, "", err
	}
	specs, err := openSpecsRoot(root)
	if err != nil {
		return nil, "", err
	}
	dir, base := path.Split(sub)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		return specs, base, nil
	}
	parent, err := specs.OpenRoot(dir)
	specs.Close()
	if err != nil {
		return nil, "", err
	}
	return parent, base, nil
}

// newTempAt creates an exclusive temp file next to the target, like CreateTemp but
// through the pinned handle. The name never comes from the caller.
func newTempAt(parent *os.Root, hint string) (string, *os.File, error) {
	for attempt := 0; attempt < 100; attempt++ {
		name := ".spec-" + randName() + ".tmp"
		f, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return name, f, nil
	}
	return "", nil, fmt.Errorf("cannot create a temp file beside %s", hint)
}

// lockAt is the change-record lock, through the pinned directory: O_EXCL create,
// removed on release. A lock that is being removed on Windows surfaces as access
// denied, so every error is retried until the deadline.
//
// It never removes an existing lock. An abandoned lock and a live writer look the
// same across processes and filesystems, and stealing a live writer's lock loses
// its update silently; failing with the lock's path lets a person remove a lock they
// know is abandoned. The deadline is short because every critical section is a
// bounded read and one same-directory rename.
func lockAt(parent *os.Root, name string) (func(), error) {
	deadline := time.Now().Add(2 * time.Second)
	var last error
	for {
		f, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return func() { _ = parent.Remove(name) }, nil
		}
		last = err
		if time.Now().After(deadline) {
			lock := name
			if parent.Name() != "" {
				lock = filepath.Join(parent.Name(), name)
			}
			return nil, fmt.Errorf("%s is locked by another writer; if no writer is still running, remove that lock file and retry: %w", lock, last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// randName is a hex nonce for temp file names.
func randName() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// saveFileAt replaces base in the pinned directory. Only a regular file is
// replaced: a final symlink or junction is refused rather than renamed over, so a
// planted link can neither receive the text nor redirect the chmod. The caller's
// lock is taken first, then replaceFileAt does the write.
func saveFileAt(parent *os.Root, base, text string) error {
	if base == "" || base == "." || base == ".." || strings.ContainsAny(base, `/\`) {
		return fmt.Errorf("%w: invalid name", ErrSpecTarget)
	}
	info, err := parent.Lstat(base)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSpecTarget, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file", ErrSpecTarget)
	}
	unlock, err := lockAt(parent, base+".lock")
	if err != nil {
		return err
	}
	defer unlock()
	return replaceFileAt(parent, base, text)
}

// replaceFileAt writes text to an exclusive temp file in the pinned directory and
// renames it over base. The caller holds the lock, so concurrent writers serialize
// and a reader never sees a truncated document.
func replaceFileAt(parent *os.Root, base, text string) error {
	name, tmp, err := newTempAt(parent, base)
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		_ = parent.Remove(name)
		return err
	}
	// Mode 0644 is what os.CreateTemp did not set and the old writer chmodded in;
	// through the handle there is no path to follow. A failure here leaves the
	// umask's stricter mode, as before.
	_ = tmp.Chmod(0o644)
	if err := tmp.Close(); err != nil {
		_ = parent.Remove(name)
		return err
	}
	if err := parent.Rename(name, base); err != nil {
		_ = parent.Remove(name)
		return err
	}
	return nil
}
