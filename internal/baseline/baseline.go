// Package baseline is the Go port of src/baseline.ts: a graph either indexed at
// a git ref in a throwaway worktree, or loaded from a saved graph.json.
package baseline

import (
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/rootio"
	"github.com/michalbasisty/weave/internal/weave"
)

// RefProblem reports why a ref cannot be handed to git verbatim, or "" when it
// can. A leading dash would read as an option; control characters have no place
// in a ref. Everything else, including Unicode, is git's to accept or reject —
// stripping characters would silently compare against a different ref.
func RefProblem(ref string) string {
	if strings.TrimSpace(ref) == "" {
		return "empty"
	}
	if strings.HasPrefix(ref, "-") {
		return "option-like"
	}
	for _, r := range ref {
		if r == 0 || r < 0x20 || r == 0x7f {
			return "control character"
		}
	}
	return ""
}

// cacheVersion is bumped when extraction changes in a way that alters a graph for
// the same commit, so old baselines are not reused.
const cacheVersion = 1

// cacheDir is the baseline cache directory, relative to the repository root.
var cacheDir = filepath.Join(".weave", "baselines")

// GraphAt indexes root at ref in a detached worktree, reusing a cached graph for
// that commit. It returns nil when the ref is unusable or git or the index fails;
// the worktree is always removed.
func GraphAt(root, ref string) *weave.Graph {
	if RefProblem(ref) != "" {
		return nil
	}
	sha := resolveSHA(root, ref)
	if sha == "" {
		return nil
	}
	name := sha + "-v" + itoa(cacheVersion) + ".json"
	if g := loadCache(root, name); g != nil {
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

	if err := git("worktree", "add", "--detach", "--quiet", tmp, ref); err != nil {
		return nil
	}
	g := weave.BuildGraph(tmp)
	if g == nil {
		return nil
	}
	g.Root = root
	if saveCache(root, name, g.MarshalJS()) == nil {
		prune(root, 20)
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

// openCache pins <root>/.weave/baselines. The repository root is pinned first and
// the cache directory is reached through it, so a .weave or baselines component
// swapped for a link out of the repository is refused rather than followed. With
// create, the directory is made through the same pinned root first. The caller
// closes the returned root.
func openCache(root string, create bool) (*os.Root, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	repo, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	defer repo.Close()
	if create {
		if err := repo.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, err
		}
	}
	return repo.OpenRoot(cacheDir)
}

// loadCache reads one cached graph through the pinned cache directory. A missing
// directory or file, a path linked out of the repository, an irregular file or
// invalid content is a miss, so the caller rebuilds.
func loadCache(root, name string) *weave.Graph {
	dir, err := openCache(root, false)
	if err != nil {
		return nil
	}
	defer dir.Close()
	f, err := rootio.OpenRegular(dir, name, os.O_RDONLY, 0)
	if err != nil {
		return nil
	}
	raw, err := io.ReadAll(f)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return nil
	}
	return decodeGraph(raw)
}

// saveCache writes one graph to the pinned cache directory, creating it when it
// is absent. It is best effort at the call site: a cache that cannot be pinned is
// left alone and the freshly built graph is still returned.
func saveCache(root, name string, data []byte) error {
	dir, err := openCache(root, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	return rootio.WriteFile(dir, name, data, 0o644)
}

// prune keeps the newest n cache files, so a long-lived checkout does not grow
// without bound. Listing and removal both go through the pinned cache directory:
// a baselines directory swapped for a link out of the repository is refused, so a
// prune cannot list or delete files outside it. Only regular .json files count as
// cache entries; a link or directory planted in the cache is neither aged nor
// removed.
func prune(root string, n int) {
	dir, err := openCache(root, false)
	if err != nil {
		return
	}
	defer dir.Close()
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil {
		return
	}
	type file struct {
		name string
		at   time.Time
	}
	var files []file
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, file{e.Name(), info.ModTime()})
	}
	if len(files) <= n {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.After(files[j].at) })
	for _, f := range files[n:] {
		_ = dir.Remove(f.name)
	}
}

// decodeGraph parses a marshalled graph. It returns nil for invalid JSON, a
// non-object, or a graph written by another version.
func decodeGraph(raw []byte) *weave.Graph {
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

// GraphFromFile reads a saved graph.json from any path the caller names, such as
// the --from flag; only the baseline cache path is pinned. It returns nil for an
// unreadable file, invalid JSON, or a graph written by another version.
func GraphFromFile(path string) *weave.Graph {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return decodeGraph(raw)
}
