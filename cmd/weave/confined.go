package main

import (
	"os"
	"path/filepath"

	"github.com/michalbasisty/weave/internal/rootio"
)

// openRepoRoot pins dir, so every later name is resolved inside that handle rather
// than against a path resolved earlier.
func openRepoRoot(dir string) (*os.Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return os.OpenRoot(abs)
}

// openRepoDir pins root/rel, creating rel through the pinned repository root first.
// A .weave directory swapped for a link out of the repo is refused, so a default
// output cannot land outside it. A link that stays inside the repo is refused too
// when it is a junction or an absolute symlink; unreachableDirErr names the path to
// replace in that case.
func openRepoDir(root, rel string) (*os.Root, error) {
	repo, err := openRepoRoot(root)
	if err != nil {
		return nil, err
	}
	defer repo.Close()
	name := filepath.FromSlash(rel)
	if err := repo.MkdirAll(name, 0o755); err != nil {
		return nil, unreachableDirErr(filepath.Join(root, name), err)
	}
	dir, err := repo.OpenRoot(name)
	if err != nil {
		return nil, unreachableDirErr(filepath.Join(root, name), err)
	}
	return dir, nil
}

// unreachableDirErr adds the repair to os.Root's "path escapes from parent" on a
// default output directory: a Windows junction or an absolute symlink is not
// followed, even one that points inside the repo. The refusal stands and nothing
// falls back to an unresolved path; the hint says to use a real directory instead.
func unreachableDirErr(path string, err error) error {
	return rootio.ExplainPathEscapes(path, err)
}
