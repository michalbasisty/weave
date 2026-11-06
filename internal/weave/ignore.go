package weave

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

// What git ignores is build output, not architecture: a generated Prisma client
// under src/generated/ is not a module of the app. git itself answers, so nested
// .gitignore files, .git/info/exclude and the global excludes all count. Tracked
// files are never in the list, and outside a git repo nothing is ignored.

type ignoreSet struct {
	dirs, files map[string]bool
}

var (
	ignoreMu    sync.Mutex
	ignoreCache = map[string]struct {
		at  time.Time
		set *ignoreSet
	}{}
)

// gitIgnored lists root's ignored paths, relative and POSIX, kept for a second so
// one command's several walks ask git once.
func gitIgnored(root string) *ignoreSet {
	ignoreMu.Lock()
	defer ignoreMu.Unlock()
	if c, ok := ignoreCache[root]; ok && time.Since(c.at) < time.Second {
		return c.set
	}
	set := &ignoreSet{dirs: map[string]bool{}, files: map[string]bool{}}
	cmd := exec.Command("git", "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil {
		for _, p := range strings.Split(string(out), "\x00") {
			if p == "" {
				continue
			}
			if strings.HasSuffix(p, "/") {
				set.dirs[strings.TrimSuffix(p, "/")] = true
			} else {
				set.files[p] = true
			}
		}
	}
	ignoreCache[root] = struct {
		at  time.Time
		set *ignoreSet
	}{time.Now(), set}
	return set
}

func (s *ignoreSet) has(rel string, dir bool) bool {
	if s == nil {
		return false
	}
	if dir {
		return s.dirs[rel]
	}
	return s.files[rel]
}
