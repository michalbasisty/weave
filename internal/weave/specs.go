package weave

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
)

// Req is one requirement found in a spec.
type Req struct {
	ID, Title string
	Desc      *string
	Change    string
	Kind      string // ADDED, MODIFIED, REMOVED or EXISTING
	File      string
	Line      int
	Hash      *string
}

// Task is one checklist item of a change's tasks.md.
type Task struct {
	ID, Change, Title string
	Done              bool
	Reqs, Evidence    []string
}

var (
	reHeading   = jsRe(`^(#{1,5})\s+(.*)$`)
	reHeadingID = jsRe(`^(?:(ADDED|MODIFIED|REMOVED)\s+)?((?:REQ|FR|NFR)-[A-Z0-9-]+)\s*[—–:-]?\s*(.*)$`)
	reBulletID  = jsRe(`^-\s+(?:\[[ xX]\]\s+)?\**((?:REQ|FR|NFR)-[A-Z0-9-]+)\**\s*[:—–-]\s*(.*)$`)
	reTaskStart = jsRe(`^-\s+\[( |x|X)\]\s+(.*)$`)
	// `T1`, `B1.1`, `1.2` or nothing at all: real task lists use all four.
	reTaskID     = jsRe(`^\**((?:[A-Z]{0,2}\d+(?:\.\d+)+)|(?:T\d+)|(?:\d+\.))\**\s+`)
	reSpecDelta  = regexp.MustCompile(`/spec-delta\.md$`)
	reCapability = regexp.MustCompile(`/capabilities/[^/]+/spec\.md$`)
	reTasksFile  = regexp.MustCompile(`/tasks\.md$`)
	reTemplates  = regexp.MustCompile(`/templates/`)
	reTaskStop   = jsRe(`(?i)^#{1,4}\s*(definition of done|checklist|verification|notes|references)`)
	reSatisfies  = regexp.MustCompile("`\\[Satisfies:[^\\]]*\\]`")
	reBacktick   = regexp.MustCompile("`([^`]+)`")
)

type reqTable struct {
	order []string
	by    map[string]*Req
}

func (t *reqTable) set(r *Req) {
	if _, ok := t.by[r.ID]; !ok {
		t.order = append(t.order, r.ID)
	}
	t.by[r.ID] = r
}

// ExtractSpecs reads requirements from spec deltas and capability specs, then the
// tasks of every change.
func ExtractSpecs(specFiles []SourceFile, b *Builder) ([]*Req, []*Task) {
	reqs := &reqTable{by: map[string]*Req{}}
	var tasks []*Task

	for i := range specFiles {
		f := &specFiles[i]
		if !reSpecDelta.MatchString(f.Rel) && !reCapability.MatchString(f.Rel) {
			continue
		}
		change := BaseName(DirOf(f.Rel))

		type open struct {
			id, title, kind string
			text            []string
			line            int
		}
		var cur *open
		flush := func() {
			if cur == nil {
				return
			}
			bodyRaw := jsstr.Trim(collapseSpace(strings.Join(cur.text, "\n")))
			body := jsstr.Trim(collapseSpace(strings.Join(cur.text[1:], " ")))
			sum := sha256.Sum256([]byte(cur.id + "\x00" + bodyRaw))
			hash := hex.EncodeToString(sum[:])[:16]
			desc := jsstr.Slice(body, 0, 220)
			prev := reqs.by[cur.id]
			if prev == nil {
				reqs.set(&Req{ID: cur.id, Title: cur.title, Desc: &desc, Change: change, Kind: cur.kind, File: f.Rel, Line: cur.line, Hash: &hash})
			} else if prev.Hash == nil || *prev.Hash == "" {
				next := *prev
				next.Hash = &hash
				if next.Title == "" {
					next.Title = cur.title
				}
				if next.Desc == nil || *next.Desc == "" {
					next.Desc = &desc
				}
				reqs.set(&next)
			}
			cur = nil
		}

		for n, line := range f.Lines {
			if head := reHeading.FindStringSubmatch(line); head != nil {
				flush()
				if hm := reHeadingID.FindStringSubmatchIndex(head[2]); hm != nil {
					h := head[2]
					kind := "EXISTING"
					if hm[2] >= 0 {
						kind = h[hm[2]:hm[3]]
					}
					cur = &open{h[hm[4]:hm[5]], jsstr.Trim(h[hm[6]:hm[7]]), kind, []string{line}, n + 1}
				}
				continue
			}
			if bullet := reBulletID.FindStringSubmatch(line); bullet != nil {
				flush()
				cur = &open{bullet[1], jsstr.Trim(bullet[2]), "EXISTING", []string{line}, n + 1}
				continue
			}
			if cur != nil {
				cur.text = append(cur.text, line)
			}
		}
		flush()

		// Requirements mentioned without a block of their own still count for traceability.
		for _, id := range reqIDs(f.Text, true) {
			if _, ok := reqs.by[id]; ok {
				continue
			}
			reqs.set(&Req{ID: id, Change: change, Kind: "EXISTING", File: f.Rel})
		}
	}

	for _, rid := range reqs.order {
		r := reqs.by[rid]
		meta := &jsjson.Object{}
		meta.Set("title", jsjson.Str(r.Title))
		if r.Desc != nil {
			meta.Set("desc", jsjson.Str(*r.Desc))
		}
		if r.Hash != nil {
			meta.Set("hash", jsjson.Str(*r.Hash))
		}
		n := &GNode{ID: ReqID(r.ID), Kind: KindReq, Name: r.ID, File: Ptr(r.File), Meta: meta}
		if r.Line != 0 {
			n.Line = Ptr(r.Line)
		}
		b.Node(n)
		b.Edge(ReqID(r.ID), FileID(r.File), "specified_in")
	}

	proposalOf := func(slug string) *SourceFile {
		for _, suffix := range []string{"/changes/" + slug + "/proposal.md", "/changes/" + slug + "/spec-delta.md"} {
			for i := range specFiles {
				if strings.HasSuffix(specFiles[i].Rel, suffix) {
					return &specFiles[i]
				}
			}
		}
		return nil
	}

	for i := range specFiles {
		f := &specFiles[i]
		if !reTasksFile.MatchString(f.Rel) || reTemplates.MatchString(f.Rel) {
			continue
		}
		change := BaseName(DirOf(f.Rel))
		desc := ""
		if doc := proposalOf(change); doc != nil {
			desc = FirstParagraph(doc.Text, 280)
		}
		b.Node(&GNode{ID: ChangeID(change), Kind: KindChange, Name: change, File: Ptr(f.Rel), Meta: strMeta("desc", desc)})
		var cur *Task
		seq := 0
		flush := func() {
			if cur != nil {
				tasks = append(tasks, cur)
			}
			cur = nil
		}
		for _, raw := range f.Lines {
			// A "Definition of done" or "Checklist" section is not a task list.
			if reTaskStop.MatchString(raw) {
				break
			}
			if tm := reTaskStart.FindStringSubmatch(raw); tm != nil {
				flush()
				seq++
				body := tm[2]
				taskID := "T" + itoa(seq)
				rest := body
				if idm := reTaskID.FindStringSubmatch(body); idm != nil {
					taskID = idm[1]
					rest = body[len(idm[0]):]
				}
				cur = &Task{
					ID:       taskID,
					Change:   change,
					Title:    jsstr.Trim(reSatisfies.ReplaceAllString(rest, "")),
					Done:     strings.ToLower(tm[1]) == "x",
					Reqs:     append([]string{}, reqIDs(raw, true)...),
					Evidence: backticks(raw),
				}
				continue
			}
			if cur == nil {
				continue
			}
			if continuation(raw) || jsstr.Trim(raw) == "" {
				cur.Reqs = append(cur.Reqs, reqIDs(raw, true)...)
				cur.Evidence = append(cur.Evidence, backticks(raw)...)
			} else {
				flush()
			}
		}
		flush()
	}

	for _, t := range tasks {
		tid := TaskID(t.Change + "/" + t.ID)
		meta := &jsjson.Object{}
		meta.Set("done", jsjson.Bool(t.Done))
		meta.Set("title", jsjson.Str(t.Title))
		b.Node(&GNode{ID: tid, Kind: KindTask, Name: t.Change + " " + t.ID, Meta: meta})
		b.Edge(ChangeID(t.Change), tid, "has_task")
		seen := map[string]bool{}
		for _, r := range t.Reqs {
			if seen[r] {
				continue
			}
			seen[r] = true
			b.Edge(tid, ReqID(r), "satisfies")
		}
	}
	out := make([]*Req, 0, len(reqs.order))
	for _, id := range reqs.order {
		out = append(out, reqs.by[id])
	}
	return out, tasks
}

// continuation is /^\s+\S/.test(raw): indented text.
func continuation(raw string) bool {
	return raw != "" && spaceAt(raw, 0) > 0 && jsstr.TrimStart(raw) != ""
}

func itoa(n int) string { return strconv.Itoa(n) }

var rePathish = regexp.MustCompile(`[/.]`)

func backticks(line string) []string {
	out := []string{}
	for _, m := range reBacktick.FindAllStringSubmatch(line, -1) {
		if rePathish.MatchString(m[1]) && !strings.Contains(m[1], " ") {
			out = append(out, m[1])
		}
	}
	return out
}

var (
	reBarePath = jsRe("^[`\\w./-]+`?\\s*[,.]?$")
	reSentence = regexp.MustCompile(`^[A-Z0-9]`)
)

// FirstParagraph is the first real sentence of a proposal: it skips the title,
// headings, tables, quotes and code.
func FirstParagraph(md string, max int) string {
	for _, raw := range jsstr.SplitLines(md) {
		t := jsstr.Trim(raw)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "|") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "```") || strings.HasPrefix(t, "-") {
			continue
		}
		if strings.HasPrefix(t, "`") || reBarePath.MatchString(t) { // a bare path is not a sentence
			continue
		}
		if !reSentence.MatchString(t) { // a wrapped continuation, not the start
			continue
		}
		if words(t) < 5 {
			continue
		}
		return jsstr.Slice(collapseSpace(t), 0, max)
	}
	return ""
}

// words is t.split(/\s+/).length for a trimmed t.
func words(t string) int {
	n := 1
	for i := 0; i < len(t); {
		if w := spaceAt(t, i); w > 0 {
			n++
			i = skipSpace(t, i)
			continue
		}
		i++
	}
	return n
}

var reEvidenceLead = regexp.MustCompile(`^\.?/`)

// ResolveEvidence resolves a task evidence path to a known repo file or, with a
// trailing slash, a directory (tries monorepo conventions).
func ResolveEvidence(path string, known, dirs map[string]bool) string {
	p := strings.TrimSuffix(reEvidenceLead.ReplaceAllString(path, ""), "/")
	candidates := []string{p, "packages/backend/src/" + p, "packages/web/src/" + p, "packages/backend/" + p, "packages/web/" + p, "specs/" + p}
	for _, c := range candidates {
		if known[c] {
			return c
		}
	}
	for _, c := range candidates {
		if dirs[c] {
			return c + "/"
		}
	}
	return ""
}

// DirIndex is every directory prefix in the repo, built once so evidence
// resolution is O(1).
func DirIndex(files []SourceFile) map[string]bool {
	dirs := map[string]bool{}
	for i := range files {
		k := files[i].Rel
		for j := strings.IndexByte(k, '/'); j > 0; {
			dirs[k[:j]] = true
			n := strings.IndexByte(k[j+1:], '/')
			if n < 0 {
				break
			}
			j = j + 1 + n
		}
	}
	return dirs
}
