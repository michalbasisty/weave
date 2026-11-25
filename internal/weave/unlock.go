package weave

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrNoLockMarker says there is no <file>.lock beside the spec: there is nothing
// to remove, and an unlock of an already-clean spec is not a failure.
var ErrNoLockMarker = errors.New("no lock marker")

// SpecUnlockWarning is printed before any marker is removed. A crashed writer's
// marker and a live writer's marker are indistinguishable from another process,
// so the operator has to make the call; the command never claims otherwise.
const SpecUnlockWarning = `STOP: a lock marker keeps a spec writer from overwriting another writer's update.
This command cannot prove the marker's owner is dead: an old, crashed marker and a
live writer's marker look exactly the same. Stop ALL weave writers first - every
CLI command, the panel (weave serve) and the MCP server - and prevent them from
restarting (close editors and agent sessions, stop hooks and services). Removing a
marker while any writer can still run risks a silently lost update.`

// UnlockSpecLock removes the legacy <file>.lock marker beside one spec file,
// through the same confined handles the writers use. rel is a Markdown path
// relative to specs/ (changes/x/proposal.md) or to the repo (specs/changes/x/
// proposal.md). The marker is removed only when it is a regular file and not a
// symbolic link; a directory, FIFO or link at that name is refused untouched.
//
// The returned path is the marker relative to the repo, for the CLI message. A
// missing marker returns ErrNoLockMarker. This is operator recovery, not automatic
// stale-lock detection: nothing here may be called by a writer or a save path.
func UnlockSpecLock(root, rel string) (string, error) {
	sub, err := specUnlockSub(rel)
	if err != nil {
		return "", err
	}
	parent, base, err := openSpecDir(root, "specs/"+sub)
	if err != nil {
		return "", err
	}
	defer parent.Close()

	lockName := base + ".lock"
	display := path.Join("specs", path.Dir(sub), lockName)
	info, err := parent.Lstat(lockName)
	if errors.Is(err, fs.ErrNotExist) {
		return display, ErrNoLockMarker
	}
	if err != nil {
		return display, fmt.Errorf("cannot inspect %s: %w", display, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return display, fmt.Errorf("%s is a symbolic link; only the regular legacy lock marker can be removed", display)
	}
	if !info.Mode().IsRegular() {
		return display, fmt.Errorf("%s is not a regular file; only the regular legacy lock marker can be removed", display)
	}
	if err := parent.Remove(lockName); err != nil {
		return display, fmt.Errorf("cannot remove %s: %w", display, err)
	}
	return display, nil
}

// specUnlockSub normalizes a spec Markdown path and applies the same shape check
// the spec readers and writers use. Both "changes/x/tasks.md" and
// "specs/changes/x/tasks.md" are accepted; an absolute path, a `..` escape or a
// non-Markdown name is refused. os.Root, not this check, is what keeps the
// operation inside specs.
func specUnlockSub(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("%w: empty or invalid path", ErrSpecTarget)
	}
	platform := filepath.FromSlash(rel)
	if filepath.IsAbs(platform) || filepath.VolumeName(platform) != "" || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%w: path must be relative to specs", ErrSpecTarget)
	}
	rel = strings.TrimPrefix(rel, "./")
	if !strings.HasPrefix(rel, "specs/") {
		rel = "specs/" + rel
	}
	sub, err := specSub(rel)
	if err != nil {
		return "", err
	}
	for _, seg := range strings.Split(sub, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", ErrSpecTarget
		}
	}
	return sub, nil
}
