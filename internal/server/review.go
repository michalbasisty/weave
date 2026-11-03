package server

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/weave"
)

// The review sheet answers the question a person actually has when an agent says
// done: "what changed since I last looked, and is it proved?" The baseline is a mark
// the person leaves, because a branch comparison is empty on a repo that commits to
// main, and comparing with HEAD goes empty the moment the work is committed.

type reviewMarks struct {
	Marks []mark `json:"marks"`
}

type mark struct {
	At  string `json:"at"`
	Ref string `json:"ref"`
	// Dirty is a fingerprint of the working tree at the moment of marking. Without it
	// an uncommitted change can never be "reviewed": HEAD does not move, so the same
	// files would show up on every visit.
	Dirty string `json:"dirty,omitempty"`
}

func reviewsPath(root string) string { return filepath.Join(root, ".weave", "reviews.log") }

// dirtySig fingerprints everything uncommitted, tracked or not.
func dirtySig(root string) string {
	out := gitOut(root, "status", "--porcelain")
	if out == "" {
		return ""
	}
	sum := sha1.Sum([]byte(out + "\n" + gitOut(root, "diff")))
	return hex.EncodeToString(sum[:8])
}

func readMarks(root string) []mark {
	raw, err := os.ReadFile(reviewsPath(root))
	if err != nil {
		return nil
	}
	var out []mark
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var m mark
		if json.Unmarshal([]byte(line), &m) == nil && m.Ref != "" {
			out = append(out, m)
		}
	}
	return out
}

func appendMark(root, ref string) error {
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(mark{At: time.Now().UTC().Format(time.RFC3339), Ref: ref, Dirty: dirtySig(root)})
	fh, err := os.OpenFile(reviewsPath(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.Write(append(b, '\n'))
	return err
}

func gitOut(root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ReviewNode is one touched node, by kind.
type ReviewNode struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	File string `json:"file"`
	ID   string `json:"id"`
}

// Review is the sheet: what moved since the mark, and what is not proved.
type Review struct {
	Mark     string       `json:"mark"`
	Commits  int          `json:"commits"`
	Files    int          `json:"files"`
	Nodes    []ReviewNode `json:"nodes"`
	Unproved []ReviewNode `json:"unproved"`
	Outside  []string     `json:"outside"`
	Tasks    []string     `json:"tasks"`
	Head     string       `json:"head"`
}

// buildReview joins git's changed files to the graph. delta alone is not enough:
// a handler edited in place gains no node, and that is exactly the change a person
// is looking for.
func buildReview(g *weave.Graph, root string) Review {
	head := gitOut(root, "rev-parse", "HEAD")
	marks := readMarks(root)
	base := head
	if len(marks) > 0 {
		base = marks[len(marks)-1].Ref
	}
	r := Review{Mark: base, Head: head}
	if base == "" || head == "" {
		return r
	}

	// The range is: committed work since the mark, plus the working tree only while it
	// still differs from the fingerprint taken at mark time.
	rangeSpec := base
	if len(marks) > 0 && marks[len(marks)-1].Dirty == dirtySig(root) {
		rangeSpec = base + "..HEAD"
	}
	out := gitOut(root, "diff", "--name-only", rangeSpec)
	files := []string{}
	for _, f := range strings.Split(out, "\n") {
		if f = strings.TrimSpace(f); f != "" && !strings.HasPrefix(f, ".weave/") {
			files = append(files, f)
		}
	}
	r.Files = len(files)
	if n := gitOut(root, "rev-list", "--count", base+"..HEAD"); n != "" {
		if v, err := json.Number(n).Int64(); err == nil {
			r.Commits = int(v)
		}
	}
	if r.Files == 0 && r.Commits == 0 {
		return r
	}

	touched := map[string]bool{}
	for _, f := range files {
		touched[f] = true
	}
	ix := weave.NewIndex(g)
	seen := map[string]bool{}
	add := func(n *weave.GNode, proved bool) {
		if n == nil || seen[n.ID] {
			return
		}
		seen[n.ID] = true
		item := ReviewNode{Kind: n.Kind, Name: n.Name, ID: n.ID}
		if n.File != nil {
			item.File = *n.File
		}
		r.Nodes = append(r.Nodes, item)
		if !proved {
			r.Unproved = append(r.Unproved, item)
		}
	}
	for _, n := range g.Nodes {
		if n.File == nil || !touched[*n.File] {
			continue
		}
		switch n.Kind {
		case weave.KindEndpoint:
			proved := len(ix.To(n.ID, "hits")) > 0 || len(ix.To(n.ID, "covers")) > 0
			add(n, proved)
		case weave.KindPage:
			add(n, len(ix.To(n.ID, "exercises")) > 0)
		case weave.KindModule, weave.KindTable:
			add(n, true)
		}
	}
	sort.SliceStable(r.Nodes, func(i, j int) bool {
		if r.Nodes[i].Kind != r.Nodes[j].Kind {
			return r.Nodes[i].Kind < r.Nodes[j].Kind
		}
		return r.Nodes[i].Name < r.Nodes[j].Name
	})
	sort.SliceStable(r.Unproved, func(i, j int) bool { return r.Unproved[i].Name < r.Unproved[j].Name })

	// Files that no change record names: work that arrived without a plan.
	named := map[string]bool{}
	for _, c := range ix.OfKind(weave.KindChange) {
		for _, te := range ix.From(c.ID, "has_task") {
			t := ix.Node(te.To)
			if t == nil {
				continue
			}
			for _, e := range ix.From(t.ID, "touches") {
				if fn := ix.Node(e.To); fn != nil && fn.File != nil {
					named[*fn.File] = true
				}
			}
		}
	}
	for _, f := range files {
		if !named[f] && !strings.Contains(f, ".md") {
			r.Outside = append(r.Outside, f)
		}
	}
	sort.Strings(r.Outside)

	// Tasks ticked in the range: the agent's claim, in its own file.
	diff := gitOut(root, "diff", "--unified=0", base, "--", "*tasks.md")
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && strings.Contains(line, "[x]") || strings.HasPrefix(line, "+") && strings.Contains(line, "[X]") {
			t := strings.TrimSpace(strings.TrimPrefix(line, "+"))
			if t != "" {
				r.Tasks = append(r.Tasks, t)
			}
		}
	}
	return r
}
