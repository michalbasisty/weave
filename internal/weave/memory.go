package weave

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
)

// Notes an agent (or a person) leaves next to files: a decision, a gotcha, the reason
// a shape is odd. They survive re-indexing because they live beside the graph, and
// they are never presented as evidence — only as a previous reader's claim.

// Note is one line of .weave/memory.jsonl.
type Note struct {
	At    string   `json:"at"`
	Files []string `json:"files"`
	Note  string   `json:"note"`
}

// MemoryPath is <root>/.weave/memory.jsonl.
func MemoryPath(root string) string {
	return filepath.Join(root, ".weave", "memory.jsonl")
}

func normNote(p string) string {
	return jsstr.Trim(strings.TrimPrefix(strings.ReplaceAll(p, `\`, "/"), "./"))
}

// ReadNotes is every well-formed note, oldest first. A missing file is no notes.
func ReadNotes(root string) ([]Note, error) {
	raw, err := os.ReadFile(MemoryPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return []Note{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Note{}
	for _, line := range strings.Split(jsstr.DecodeUTF8(raw), "\n") {
		if jsstr.Trim(line) == "" {
			continue
		}
		// a torn line from a concurrent write: skip it, never lose the rest
		v, err := jsjson.Parse([]byte(line))
		if err != nil || v.Kind() != jsjson.KindObject {
			continue
		}
		o := v.Object()
		note, _ := o.Get("note")
		files, _ := o.Get("files")
		at, _ := o.Get("at")
		if note.Kind() != jsjson.KindString || files.Kind() != jsjson.KindArray || at.Kind() != jsjson.KindString {
			continue
		}
		list := make([]string, len(files.Array()))
		for i, f := range files.Array() {
			list[i] = jsString(f) // the TypeScript only checks that files is an array
		}
		out = append(out, Note{At: at.Str(), Files: list, Note: note.Str()})
	}
	return out, nil
}

// Remember appends a note for files, relative to root. now "" is the current
// time. It returns nil when there is no file or no text to keep.
func Remember(root string, files []string, note, now string) (*Note, error) {
	base := strings.TrimSuffix(strings.ReplaceAll(root, `\`, "/"), "/") + "/"
	list := newSet()
	for _, f := range files {
		f = normNote(f)
		if strings.HasPrefix(f, base) {
			f = f[len(base):]
		}
		if f != "" {
			list.add(f)
		}
	}
	text := jsstr.Slice(jsstr.Trim(note), 0, 1000)
	if list.size() == 0 || text == "" {
		return nil, nil
	}
	if now == "" {
		now = isoNow()
	}
	entry := &Note{At: now, Files: list.items(), Note: text}
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		return nil, err
	}
	w := objWriter{}
	w.str("at", entry.At)
	w.strs("files", entry.Files)
	w.str("note", entry.Note)
	fh, err := os.OpenFile(MemoryPath(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err := fh.Write(append(w.end(), '\n')); err != nil {
		fh.Close()
		return nil, err
	}
	return entry, fh.Close()
}

// matching is the notes naming any of files (every note when files is empty),
// newest first, at most n.
func matching(notes []Note, files []string, n int) []Note {
	want := map[string]bool{}
	for _, f := range files {
		want[normNote(f)] = true
	}
	hits := []Note{}
	for _, note := range notes {
		ok := len(want) == 0
		for i := 0; i < len(note.Files) && !ok; i++ {
			ok = want[normNote(note.Files[i])]
		}
		if ok {
			hits = append(hits, note)
		}
	}
	hits = hits[max(0, len(hits)-n):]
	for i, j := 0, len(hits)-1; i < j; i, j = i+1, j-1 {
		hits[i], hits[j] = hits[j], hits[i]
	}
	return hits
}

// NotesFor is the newest three notes naming any of files. An unreadable memory
// file reads as no notes: the context pack must not fail on it.
func NotesFor(root string, files []string) []Note {
	if len(files) == 0 {
		return []Note{}
	}
	notes, err := ReadNotes(root)
	if err != nil {
		return []Note{}
	}
	return matching(notes, files, 3)
}

// RecallText is the newest twenty notes, for files or for the whole repo.
func RecallText(root string, files []string) (string, error) {
	all, err := ReadNotes(root)
	if err != nil {
		return "", err
	}
	notes := matching(all, files, 20)
	if len(notes) == 0 {
		if len(files) > 0 {
			return "no note for " + strings.Join(files, ", "), nil
		}
		return "no notes yet — write one with: weave remember --note \"…\" <file>", nil
	}
	head := "recall: " + strconv.Itoa(len(notes)) + " note(s), newest first"
	if len(files) > 0 {
		head = "recall: " + strconv.Itoa(len(notes)) + " note(s) for " + strings.Join(files, ", ")
	}
	out := []string{head}
	for _, n := range notes {
		out = append(out, "  "+jsstr.Slice(n.At, 0, 10)+"  "+n.Note+"  ["+strings.Join(n.Files, ", ")+"]")
	}
	return strings.Join(out, "\n"), nil
}

// ClearMemory empties the memory file and returns how many notes it held.
func ClearMemory(root string) (int, error) {
	notes, err := ReadNotes(root)
	if err != nil {
		return 0, err
	}
	return len(notes), os.WriteFile(MemoryPath(root), nil, 0o644)
}
