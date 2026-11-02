// Package baseline is the Go port of src/baseline.ts: a graph either indexed at
// a git ref in a throwaway worktree, or loaded from a saved graph.json.
package baseline

import (
	"os"
	"os/exec"
	"regexp"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/weave"
)

// okRef is the ref a git command may see: the TypeScript strips every other character.
var okRef = regexp.MustCompile(`[^A-Za-z0-9_./~^-]`)

// GraphAt indexes root at ref in a detached worktree. It returns nil when git or
// the index fails; the worktree is always removed.
func GraphAt(root, ref string) *weave.Graph {
	safe := okRef.ReplaceAllString(ref, "")
	if safe == "" {
		return nil
	}
	tmp, err := os.MkdirTemp("", "weave-base-")
	if err != nil {
		return nil
	}
	// git worktree add wants to create the path itself.
	_ = os.Remove(tmp)

	git := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		return cmd.Run()
	}
	defer func() {
		_ = git("worktree", "remove", "--force", tmp)
		_ = git("worktree", "prune")
	}()

	if err := git("worktree", "add", "--detach", "--quiet", tmp, safe); err != nil {
		return nil
	}
	return weave.BuildGraph(tmp)
}

// GraphFromFile reads a saved graph.json. It returns nil for an unreadable file,
// invalid JSON, or a graph written by another version.
func GraphFromFile(path string) *weave.Graph {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	v, err := jsjson.Parse([]byte(jsstr.DecodeUTF8(raw)))
	if err != nil {
		return nil
	}
	o := v.Object()
	if o == nil {
		return nil
	}
	ver, ok := o.Get("version")
	if !ok || ver.Kind() != jsjson.KindNumber || ver.Float() != weave.GraphVersion {
		return nil
	}
	g, err := weave.DecodeGraph(v)
	if err != nil {
		return nil
	}
	return g
}
