package grep

import (
	"os"
	"strings"
	"sync"

	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/weave"
)

// A long-lived MCP server answers text searches repeatedly. Walking and reading
// 937 files each time cost ~240 ms; keeping the lines and re-reading only files
// whose size or mtime changed makes later calls a memory scan.

type cachedLines struct {
	rel    string
	lines  []string
	lower  string
	starts []int
}

type fileEntry struct {
	size   int64
	mtime  int64
	lines  []string
	lower  string
	starts []int
}

type Searcher struct {
	mu      sync.Mutex
	root    string
	entries map[string]*fileEntry
}

var (
	searcherMu sync.Mutex
	searchers  = map[string]*Searcher{}
)

// Cached is Grep with a per-root text cache. It is safe for concurrent use.
func Cached(g *weave.Graph, query string, limit int, useRegex bool) string {
	searcherMu.Lock()
	s := searchers[g.Root]
	if s == nil {
		s = &Searcher{root: g.Root, entries: map[string]*fileEntry{}}
		searchers[g.Root] = s
	}
	searcherMu.Unlock()
	return s.grep(g, query, limit, useRegex)
}

func (s *Searcher) snapshot() []cachedLines {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.entries == nil {
		s.entries = map[string]*fileEntry{}
	}
	seen := map[string]bool{}
	out := make([]cachedLines, 0, len(s.entries))
	for _, info := range weave.WalkInfo(s.root) {
		seen[info.Rel] = true
		cur := s.entries[info.Rel]
		if cur == nil || cur.size != info.Size || cur.mtime != info.Mtime {
			raw, err := os.ReadFile(info.Abs)
			if err != nil {
				delete(s.entries, info.Rel)
				continue
			}
			text := jsstr.DecodeUTF8(raw)
			lower := strings.ToLower(text)
			starts := []int{0}
			for i := 0; i < len(lower); i++ {
				if lower[i] == '\n' {
					starts = append(starts, i+1)
				}
			}
			cur = &fileEntry{size: info.Size, mtime: info.Mtime, lines: jsstr.SplitLines(text), lower: lower, starts: starts}
			s.entries[info.Rel] = cur
		}
		out = append(out, cachedLines{rel: info.Rel, lines: cur.lines, lower: cur.lower, starts: cur.starts})
	}
	for rel := range s.entries {
		if !seen[rel] {
			delete(s.entries, rel)
		}
	}
	return out
}

func (s *Searcher) grep(g *weave.Graph, query string, limit int, useRegex bool) string {
	files := s.snapshot()
	return render(g, query, limit, useRegex, files)
}
