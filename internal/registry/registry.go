// Package registry is the Go port of src/registry.ts: projects live in one small
// file at ~/.weave/projects.json, and using a repo registers it.
package registry

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
)

// Entry is one known project. Files, Endpoints and Changes are nil until the
// first index, which is JS undefined: the key is then left out of the file.
type Entry struct {
	Name      string `json:"name"`
	Root      string `json:"root"`
	LastUsed  string `json:"lastUsed"`
	Files     *int   `json:"files,omitempty"`
	Endpoints *int   `json:"endpoints,omitempty"`
	Changes   *int   `json:"changes,omitempty"`
}

// Counts is what the last index found, for the project list.
type Counts struct {
	Files     int
	Endpoints int
	Changes   int
}

// HomeDir is WEAVE_HOME, or ~/.weave.
func HomeDir() string {
	if h := os.Getenv("WEAVE_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return filepath.Join(home, ".weave")
}

func file() string { return filepath.Join(HomeDir(), "projects.json") }

// Load reads the registry; a missing or malformed file reads as no projects.
func Load() []Entry {
	raw, err := os.ReadFile(file())
	if err != nil {
		return []Entry{}
	}
	v, err := jsjson.Parse([]byte(jsstr.DecodeUTF8(raw)))
	if err != nil {
		return []Entry{}
	}
	root := v.Object()
	if root == nil {
		return []Entry{}
	}
	pv, ok := root.Get("projects")
	if !ok || pv.Kind() != jsjson.KindArray {
		return []Entry{}
	}
	out := make([]Entry, 0, len(pv.Array()))
	for _, item := range pv.Array() {
		o := item.Object()
		if o == nil {
			continue
		}
		e := Entry{Name: fieldStr(o, "name"), Root: fieldStr(o, "root"), LastUsed: fieldStr(o, "lastUsed")}
		e.Files = fieldInt(o, "files")
		e.Endpoints = fieldInt(o, "endpoints")
		e.Changes = fieldInt(o, "changes")
		out = append(out, e)
	}
	return out
}

// Save writes at most 200 projects as 2-space JSON plus a trailing newline.
// A read-only home must not break the command, so errors are ignored.
func Save(entries []Entry) {
	if len(entries) > 200 {
		entries = entries[:200]
	}
	if err := os.MkdirAll(HomeDir(), 0o755); err != nil {
		return
	}
	projects := make([]jsjson.Value, len(entries))
	for i, e := range entries {
		o := &jsjson.Object{}
		o.Set("name", jsjson.Str(e.Name))
		o.Set("root", jsjson.Str(e.Root))
		o.Set("lastUsed", jsjson.Str(e.LastUsed))
		if e.Files != nil {
			o.Set("files", jsjson.Int(*e.Files))
		}
		if e.Endpoints != nil {
			o.Set("endpoints", jsjson.Int(*e.Endpoints))
		}
		if e.Changes != nil {
			o.Set("changes", jsjson.Int(*e.Changes))
		}
		projects[i] = jsjson.Obj(o)
	}
	root := &jsjson.Object{}
	root.Set("projects", jsjson.Arr(projects...))
	out := prettyJSON(nil, jsjson.Obj(root), 0)
	out = append(out, '\n')
	_ = os.WriteFile(file(), out, 0o644)
}

// Touch remembers a repo the moment it is used. The same root updates its
// timestamp; the same folder name on another root gets a -2, -3 suffix.
func Touch(root string) Entry {
	entries := Load()
	want := norm(root)
	for i := range entries {
		if norm(entries[i].Root) == want {
			entries[i].LastUsed = now()
			Save(entries)
			return entries[i]
		}
	}
	base := baseName(root)
	if base == "" {
		base = "project"
	}
	name := base
	for i := 2; taken(entries, name); i++ {
		name = base + "-" + strconv.Itoa(i)
	}
	entry := Entry{Name: name, Root: root, LastUsed: now()}
	entries = append(entries, entry)
	Save(entries)
	return entry
}

// NoteIndex records what the last index found, if the repo is registered.
func NoteIndex(root string, counts Counts) {
	entries := Load()
	want := norm(root)
	for i := range entries {
		if norm(entries[i].Root) != want {
			continue
		}
		files, endpoints, changes := counts.Files, counts.Endpoints, counts.Changes
		entries[i].Files = &files
		entries[i].Endpoints = &endpoints
		entries[i].Changes = &changes
		entries[i].LastUsed = now()
		Save(entries)
		return
	}
}

// Candidates is every entry whose name or folder matches, so the caller can
// explain ambiguity. A prefix match wins over a mere contained match.
func Candidates(name string) []Entry {
	q := key(name)
	if q == "" {
		return []Entry{}
	}
	all := Load()
	starts := make([]Entry, 0)
	for _, e := range all {
		if strings.HasPrefix(key(e.Name), q) || strings.HasPrefix(key(baseName(e.Root)), q) {
			starts = append(starts, e)
		}
	}
	if len(starts) > 0 {
		return starts
	}
	out := make([]Entry, 0)
	for _, e := range all {
		if strings.Contains(key(e.Name), q) || strings.Contains(key(baseName(e.Root)), q) {
			out = append(out, e)
		}
	}
	return out
}

// Resolve is an exact name, or an unambiguous prefix, or nil.
func Resolve(name string) *Entry {
	q := key(name)
	entries := Load()
	exact := make([]int, 0, 1)
	for i := range entries {
		if key(entries[i].Name) == q {
			exact = append(exact, i)
		}
	}
	if len(exact) == 1 {
		return &entries[exact[0]]
	}
	hits := Candidates(name)
	if len(hits) == 1 {
		return &hits[0]
	}
	return nil
}

// Forget removes the entry with exactly this name; false if there was none.
func Forget(name string) bool {
	entries := Load()
	next := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Name != name {
			next = append(next, e)
		}
	}
	if len(next) == len(entries) {
		return false
	}
	Save(next)
	return true
}

// Table is `weave projects`: one line per repo, newest first.
func Table(entries []Entry) string {
	if len(entries) == 0 {
		return "no projects yet — run `weave` inside a repo"
	}
	rows := append([]Entry(nil), entries...)
	sort.SliceStable(rows, func(i, j int) bool {
		return jsstr.LocaleCompare(rows[i].LastUsed, rows[j].LastUsed) > 0
	})
	width := 4
	for _, r := range rows {
		if n := jsstr.Len(r.Name); n > width {
			width = n
		}
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, jsstr.PadEnd(r.Name, width)+"  "+jsstr.PadEnd(stats(r), 34)+"  "+r.Root)
	}
	header := jsstr.PadEnd("name", width) + "  " + jsstr.PadEnd("size", 34) + "  path"
	return strings.Join(append([]string{header}, lines...), "\n")
}

// Exists reports whether root is there.
func Exists(root string) bool {
	_, err := os.Stat(root)
	return err == nil
}

func stats(e Entry) string {
	if e.Files == nil || *e.Files == 0 {
		return "not indexed"
	}
	endpoints, changes := 0, 0
	if e.Endpoints != nil {
		endpoints = *e.Endpoints
	}
	if e.Changes != nil {
		changes = *e.Changes
	}
	return jsstr.PadStart(strconv.Itoa(*e.Files), 4) + " files · " +
		jsstr.PadStart(strconv.Itoa(endpoints), 3) + " ep · " +
		jsstr.PadStart(strconv.Itoa(changes), 2) + " changes"
}

// norm is the comparison key for roots: slashes, no trailing slash, lower case.
func norm(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	return strings.ToLower(p)
}

// baseName is path.basename after the registry's slash normalization. Like Node
// on Windows it treats a drive root ("C:", "C:/") as having no base, and "C:x"
// as the relative path "x".
func baseName(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	if len(p) >= 2 && p[1] == ':' && (p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z') {
		p = p[2:]
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// key is the fuzzy lookup form: lower case, letters and digits only.
func key(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func taken(entries []Entry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func fieldStr(o *jsjson.Object, k string) string {
	v, ok := o.Get(k)
	if !ok {
		return ""
	}
	return v.Str()
}

func fieldInt(o *jsjson.Object, k string) *int {
	v, ok := o.Get(k)
	if !ok || v.Kind() != jsjson.KindNumber {
		return nil
	}
	n := int(v.Float())
	return &n
}

func prettyJSON(dst []byte, v jsjson.Value, depth int) []byte {
	switch v.Kind() {
	case jsjson.KindObject:
		o := v.Object()
		if o.Len() == 0 {
			return append(dst, "{}"...)
		}
		dst = append(dst, "{\n"...)
		for i := 0; i < o.Len(); i++ {
			k, val := o.At(i)
			dst = append(dst, strings.Repeat(" ", depth+2)...)
			dst = jsjson.AppendString(dst, k)
			dst = append(dst, ':', ' ')
			dst = prettyJSON(dst, val, depth+2)
			if i < o.Len()-1 {
				dst = append(dst, ',')
			}
			dst = append(dst, '\n')
		}
		dst = append(dst, strings.Repeat(" ", depth)...)
		return append(dst, '}')
	case jsjson.KindArray:
		a := v.Array()
		if len(a) == 0 {
			return append(dst, "[]"...)
		}
		dst = append(dst, "[\n"...)
		for i, x := range a {
			dst = append(dst, strings.Repeat(" ", depth+2)...)
			dst = prettyJSON(dst, x, depth+2)
			if i < len(a)-1 {
				dst = append(dst, ',')
			}
			dst = append(dst, '\n')
		}
		dst = append(dst, strings.Repeat(" ", depth)...)
		return append(dst, ']')
	}
	return jsjson.Append(dst, v)
}
