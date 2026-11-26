// Package rootio writes repository-pinned files through an *os.Root.
//
// A Root pins a directory handle, and every operation resolves names inside that
// handle rather than against a path resolved earlier. A directory component
// swapped for a symlink after a check therefore cannot redirect a write, and a
// relative symlink that leaves the root is refused.
//
// A Root follows only a link whose target it can resolve as a relative name inside
// the root. An absolute target is refused, which on Windows also refuses a junction
// reached as a component of a name: a junction stores its target as an absolute
// path. Go reports both with an unexported error whose text is "path escapes from
// parent", even when the link points inside the root, so that text means the name
// was not resolved, not that it left the root (PathEscapes and ExplainPathEscapes
// below). The root directory itself may be a junction or be reached through one:
// OpenRoot resolves the path once with ordinary OS rules, and only names inside the
// pinned handle are confined.
//
// The helpers here check the opened handle, not just the name: a FIFO, directory
// or device is rejected before any write, and WriteFile truncates the same handle
// that it checked and then wrote. Parent directories are never created.
//
// Hard links are not confined by os.Root and are not claimed to be: a hard link
// inside the root shares the inode of a file outside it, and writing through that
// name writes the shared file. os.Root confines names and symlink traversal, not
// inode identity.
package rootio

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

var (
	// ErrTruncate refuses O_TRUNC. WriteFile truncates the checked handle
	// itself, and an unchecked truncating open could empty a target between the
	// open and the regular-file check.
	ErrTruncate = errors.New("rootio: O_TRUNC is not allowed")
	// ErrNotRegular refuses an opened target that is not a regular file. The
	// check runs on the open handle, so a FIFO is refused instead of waited on
	// and a name swapped after the open cannot change the type that was checked.
	ErrNotRegular = errors.New("rootio: not a regular file")
)

// pathEscapesText is the message of os.Root's unexported error for a name or link
// target it will not resolve. Go does not export that error, so its text is the
// only handle on it; a release that rewords the message must update this constant
// and the tests that pin the refusal.
const pathEscapesText = "path escapes from parent"

// PathEscapes reports whether err is os.Root's refusal to resolve a name. Root
// returns it for a path that leaves the root, and also for an absolute symlink
// target or a junction inside the root, even one that points back into it. So the
// answer means "the name was not resolved", never "the path left the root".
func PathEscapes(err error) bool {
	return err != nil && strings.Contains(err.Error(), pathEscapesText)
}

// ExplainPathEscapes returns err unchanged unless PathEscapes reports it. In that
// case it adds the repair os.Root's text does not give: a Windows junction (or
// another reparse point read as a link) and an absolute symlink are not followed
// even when they point inside the root, so the link has to be replaced with a real
// directory or file. The original refusal stays wrapped, and nothing here retries
// the operation outside the root.
func ExplainPathEscapes(name string, err error) error {
	if !PathEscapes(err) {
		return err
	}
	return fmt.Errorf("%s is not reachable through the pinned root: %w; a Windows junction or an absolute symlink is not followed even when it points inside the repo, so replace it with a real directory or file", name, err)
}

// OpenRegular opens name inside r and returns a handle only when the opened
// target is a regular file. O_TRUNC is refused; use WriteFile to replace content.
// The platform's non-blocking flag is added to the open, so a FIFO planted at the
// name is rejected by the handle check rather than blocking the open. No parent
// directory is created.
func OpenRegular(r *os.Root, name string, flags int, perm fs.FileMode) (*os.File, error) {
	if flags&os.O_TRUNC != 0 {
		return nil, fmt.Errorf("open %s: %w", name, ErrTruncate)
	}
	f, err := r.OpenFile(name, flags|nonblockOpen, perm)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("open %s: %w", name, ErrNotRegular)
	}
	return f, nil
}

// WriteFile replaces name with data through r. The file is opened without
// O_TRUNC, the opened handle is checked to be a regular file, and only then is
// that same handle truncated and written. Write and close failures are returned;
// a truncate failure is reported with the close failure when both occur. Parent
// directories are not created.
func WriteFile(r *os.Root, name string, data []byte, perm fs.FileMode) error {
	f, err := OpenRegular(r, name, os.O_WRONLY|os.O_CREATE, perm)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return errors.Join(err, f.Close())
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

// AppendFile appends data to name through r, creating the file when it does not
// exist. The opened handle is checked to be a regular file before the write, so a
// FIFO or other irregular target receives nothing and does not block. Write and
// close failures are returned. Parent directories are not created.
func AppendFile(r *os.Root, name string, data []byte, perm fs.FileMode) error {
	f, err := OpenRegular(r, name, os.O_WRONLY|os.O_APPEND|os.O_CREATE, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}
