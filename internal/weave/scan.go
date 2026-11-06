package weave

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michalbasisty/weave/internal/jsstr"
)

var skip = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true, "coverage": true, ".next": true, ".output": true, "out": true,
	"vendor": true, ".turbo": true, ".weave": true, ".cache": true, "tmp": true, ".nyc_output": true, "playwright-report": true,
	"test-results": true, ".gradle": true, "Pods": true, "DerivedData": true,
}

var code = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".vue": true, ".svelte": true,
	".prisma": true, ".md": true, ".json": true, ".graphql": true, ".gql": true,
}

// MaxBytes is the largest file the walk reads.
const MaxBytes = 512 * 1024

// SourceFile is one file the walk read. Rel is repo-relative and POSIX; Abs uses
// the OS separator, as path.join does in Node.
type SourceFile struct {
	Rel   string
	Abs   string
	Text  string
	Lines []string
}

func skipped(name string) bool { return skip[name] || strings.HasPrefix(name, ".") }

// readDir is readdirSync(dir).sort(): names in UTF-16 code unit order, the same
// on every OS (raw readdirSync returns NTFS order on Windows, docs/GO-MIGRATION.md
// finding 1).
func readDir(dir string) []os.DirEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return jsstr.CompareUTF16(entries[i].Name(), entries[j].Name()) < 0
	})
	return entries
}

// Walk reads every code file under root, depth first, skipping SKIP names,
// dot names and files over MaxBytes.
func Walk(root string) []SourceFile {
	var out []SourceFile
	var visit func(dir, rel string)
	visit = func(dir, rel string) {
		for _, e := range readDir(dir) {
			name := e.Name()
			if skipped(name) {
				continue
			}
			abs := filepath.Join(dir, name)
			st, err := os.Stat(abs) // follows symlinks, as statSync does
			if err != nil {
				continue
			}
			r := ToPosix(name)
			if rel != "" {
				r = rel + "/" + r
			}
			if st.IsDir() {
				visit(abs, r)
			} else if (st.Size() <= MaxBytes && code[Ext(name)]) || (specFile(r) && st.Size() <= SpecMaxBytes) {
				b, err := os.ReadFile(abs)
				if err != nil {
					continue
				}
				text := jsstr.DecodeUTF8(b)
				out = append(out, SourceFile{Rel: r, Abs: abs, Text: text, Lines: jsstr.SplitLines(text)})
			}
		}
	}
	visit(root, "")
	return out
}

// FileInfo is a file the walk would read, without reading it. Mtime is UnixNano.
type FileInfo struct {
	Rel   string
	Abs   string
	Size  int64
	Mtime int64
}

// WalkInfo lists the files Walk would read, with size and modification time but
// no contents. Long-lived callers use it to re-read only what changed.
func WalkInfo(root string) []FileInfo {
	var out []FileInfo
	var visit func(dir, rel string)
	visit = func(dir, rel string) {
		for _, e := range readDir(dir) {
			name := e.Name()
			if skipped(name) {
				continue
			}
			abs := filepath.Join(dir, name)
			st, err := os.Stat(abs)
			if err != nil {
				continue
			}
			r := ToPosix(name)
			if rel != "" {
				r = rel + "/" + r
			}
			if st.IsDir() {
				visit(abs, r)
			} else if (st.Size() <= MaxBytes && code[Ext(name)]) || (specFile(r) && st.Size() <= SpecMaxBytes) {
				out = append(out, FileInfo{Rel: r, Abs: abs, Size: st.Size(), Mtime: st.ModTime().UnixNano()})
			}
		}
	}
	visit(root, "")
	return out
}

// NewestMtime is the newest file modification time under root in milliseconds,
// without reading any file. Directories do not count.
func NewestMtime(root string) float64 {
	var max float64
	var visit func(dir string)
	visit = func(dir string) {
		for _, e := range readDir(dir) {
			if skipped(e.Name()) {
				continue
			}
			abs := filepath.Join(dir, e.Name())
			st, err := os.Stat(abs)
			if err != nil {
				continue
			}
			if st.IsDir() {
				visit(abs)
			} else if m := mtimeMs(st); m > max {
				max = m
			}
		}
	}
	visit(root)
	return max
}

// mtimeMs is Node's stat.mtimeMs: sec * 1e3 + nsec / 1e6.
func mtimeMs(st os.FileInfo) float64 {
	t := st.ModTime()
	return float64(t.Unix())*1e3 + float64(t.Nanosecond())/1e6
}

// Ext is the lower-cased extension including the dot, or "". Lower-casing is
// Unicode-simple; JS differs only for characters that never occur in an extension
// in the CODE set.
func Ext(p string) string {
	i := strings.LastIndexByte(p, '.')
	if i < 0 {
		return ""
	}
	return strings.ToLower(p[i:])
}

// ToPosix replaces every backslash with a slash.
func ToPosix(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// StripExt drops everything from the last dot.
func StripExt(p string) string {
	if i := strings.LastIndexByte(p, '.'); i >= 0 {
		return p[:i]
	}
	return p
}

// DirOf is everything before the last slash, or "".
func DirOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// BaseName is everything after the last slash.
func BaseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Rel is path.posix.join: empty parts are dropped, the result is normalized, an
// empty result is ".", and a trailing slash is kept.
func Rel(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	joined := strings.Join(kept, "/")
	if joined == "" {
		return "."
	}
	c := path.Clean(joined)
	if strings.HasSuffix(joined, "/") && c != "/" {
		if c == "." {
			return "./"
		}
		return c + "/"
	}
	return c
}
