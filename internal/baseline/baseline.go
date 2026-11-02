// Package baseline is the Go port of src/baseline.ts: a graph either indexed at
// a git ref in a throwaway worktree, or loaded from a saved graph.json.
package baseline

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/weave"
)

// okRef is the ref a git command may see: the TypeScript strips every other character.
var okRef = regexp.MustCompile(`[^A-Za-z0-9_./~^-]`)

// cacheVersion is bumped when extraction changes in a way that alters a graph for
// the same commit, so old baselines are not reused.
const cacheVersion = 1

// GraphAt indexes root at ref in a detached worktree, reusing a cached graph for
// that commit. It returns nil when git or the index fails; the worktree is always
// removed.
func GraphAt(root, ref string) *weave.Graph {
	safe := okRef.ReplaceAllString(ref, "")
	if safe == "" {
		return nil
	}
	sha := resolveSHA(root, safe)
	if sha == "" {
		return nil
	}
	cache := filepath.Join(root, ".weave", "baselines", sha+"-v"+itoa(cacheVersion)+".json")
	if g := GraphFromFile(cache); g != nil {
		g.Root = root
		return g
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
	g := weave.BuildGraph(tmp)
	if g == nil {
		return nil
	}
	g.Root = root
	if os.MkdirAll(filepath.Dir(cache), 0o755) == nil {
		_ = os.WriteFile(cache, g.MarshalJS(), 0o644)
		prune(filepath.Dir(cache), 20)
	}
	return g
}

func resolveSHA(root, ref string) string {
	cmd := exec.Command("git", "rev-parse", "--verify", ref+"^{commit}")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// prune keeps the newest n cache files, so a long-lived checkout does not grow
// without bound.
func prune(dir string, n int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		name string
		at   time.Time
	}
	var files []file
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, file{e.Name(), info.ModTime()})
	}
	if len(files) <= n {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.After(files[j].at) })
	for _, f := range files[n:] {
		_ = os.Remove(filepath.Join(dir, f.name))
	}
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
