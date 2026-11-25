// Package verify is the gate: what a diff breaks, which endpoints it leaves
// untested, and the commands that prove it. `weave check` and the MCP `verify`
// tool both read one Result, so the two can never disagree.
package verify

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/michalbasisty/weave/internal/baseline"
	"github.com/michalbasisty/weave/internal/junit"
	"github.com/michalbasisty/weave/internal/weave"
)

// Result is everything the gate found. Scope is the text after `check: `.
type Result struct {
	Scope    string
	Errors   []string
	Warnings []string
	Commands []string
	Changes  []string
	// Operational lists inputs the gate could not read (an explicit --against ref,
	// a git range, the change set). They are not contract failures: the run could
	// not judge the change at all, so it must never render as a pass.
	Operational []string
	// Broken lists each removed endpoint that something still calls, with the
	// screens that reach the caller. Pages and Endpoints are what the change touches.
	Broken    []Broken
	Pages     []string
	Endpoints []string
}

// Code is the process status for a result: 0 pass, 1 contract failure, 2 when
// the gate could not run.
func (r Result) Code() int {
	if len(r.Operational) > 0 {
		return 2
	}
	if len(r.Errors) > 0 {
		return 1
	}
	return 0
}

// Passed is true only when the gate ran and found no contract failure.
func (r Result) Passed() bool {
	return len(r.Operational) == 0 && len(r.Errors) == 0
}

// Broken is one removed endpoint that a live call site still names.
type Broken struct {
	Endpoint string
	Caller   string
	Screens  []string
}

// Text renders the result the way `weave check` prints it: one scope line, then
// any operational error, FAIL, warn and run lines. `error` and `FAIL` are
// deliberately different words: one is a broken change, the other a gate that
// could not read its input.
func (r Result) Text() string {
	lines := make([]string, 0, 1+len(r.Operational)+len(r.Errors)+len(r.Warnings)+len(r.Commands))
	lines = append(lines, "check: "+r.Scope)
	for _, e := range r.Operational {
		lines = append(lines, "  error "+e)
	}
	for _, e := range r.Errors {
		lines = append(lines, "  FAIL "+e)
	}
	for _, w := range r.Warnings {
		lines = append(lines, "  warn "+w)
	}
	for _, c := range r.Commands {
		lines = append(lines, "  run  "+c)
	}
	return strings.Join(lines, "\n")
}

// Run gates an already-built graph. An empty files list means the git change set,
// and an empty against means the last local index.
//
// A change set the gate cannot read is operational, not a pass: when git fails in
// a work tree, or an explicit --against ref has no baseline, Run returns a Result
// with Operational set and no verdict. An --against ref must pass to git verbatim:
// one with unsupported characters or a leading dash is rejected, never rewritten.
// Explicit files still work without git.
func Run(g *weave.Graph, root, against string, strict bool, files []string) Result {
	if against != "" {
		// Pass the ref to git verbatim or refuse it. A leading dash can read as an
		// option; a control character has no place in a ref. Everything else,
		// including Unicode, is left for git to accept or reject.
		if problem := baseline.RefProblem(against); problem != "" {
			return Result{
				Scope:       "the change set could not be read",
				Operational: []string{fmt.Sprintf("could not read %s — not a usable git ref (%s)", against, problem)},
			}
		}
		// `against...HEAD` is only meaningful at a merge base. A ref from an
		// unrelated history has none, so no diff can be read and no removal can be
		// judged. That is operational even when files are named explicitly: the ref
		// was a promise to gate against it, never a pass.
		if problem := mergeBaseProblem(root, against); problem != "" {
			return Result{
				Scope:       "the change set could not be read",
				Operational: []string{fmt.Sprintf("could not read %s — %s", against, problem)},
			}
		}
	}

	var errors, warnings []string
	explicit := len(files) > 0
	localChanges := false

	if !explicit {
		changed, err := discoverChanges(root, against)
		if err != nil {
			if against != "" || gitMissing() || inWorkTree(root) {
				return Result{
					Scope:       "the change set could not be read",
					Operational: []string{err.Error()},
				}
			}
			// No git work tree: the last local index is all there is, exactly as
			// before this command learned to read git status directly.
		} else {
			files = changed
			localChanges = len(changed) > 0
		}
	} else if against == "" {
		// With explicit files, git is only consulted to decide whether the committed
		// tree also moved; a non-git checkout is fine and simply skips that step.
		if changed, err := discoverChanges(root, ""); err == nil {
			localChanges = len(changed) > 0
		}
	}

	res := Result{Changes: files}
	var f *weave.Facts
	if len(files) > 0 {
		f = weave.Analyze(g, files)
	}

	var prev []string
	var baseGraph *weave.Graph // the graph removed endpoints are read from, when there is one
	if against != "" {
		base := baseline.GraphAt(root, against)
		if base == nil {
			// An explicit ref is a promise the gate can judge the change against it.
			// Falling back to the local index here could hide a removal and show a
			// pass, so the run is operational instead.
			return Result{
				Scope:       fmt.Sprintf("%d file(s) named vs %s", len(files), against),
				Changes:     files,
				Warnings:    warnings,
				Operational: []string{fmt.Sprintf("could not read %s — refusing to gate a change without its baseline", against)},
			}
		}
		baseGraph = base
		prev = endpointNames(base)
	} else {
		prev = g.PrevEndpoints
		// The last index moves with every run, so a second `weave check` on the same
		// edit would compare the edit with itself and pass. The committed tree does
		// not move: with uncommitted changes, an endpoint gone since HEAD counts too.
		if localChanges {
			if base := baseline.GraphAt(root, "HEAD"); base != nil {
				baseGraph = base
				seen := make(map[string]bool, len(prev))
				for _, e := range prev {
					seen[e] = true
				}
				for _, e := range endpointNames(base) {
					if !seen[e] {
						prev = append(prev, e)
						seen[e] = true
					}
				}
			}
		}
	}
	now := endpointNames(g)
	nowSet := make(map[string]bool, len(now))
	for _, e := range now {
		nowSet[e] = true
	}
	var removed []string
	for _, e := range prev {
		if !nowSet[e] {
			removed = append(removed, e)
		}
	}

	type brokenCall struct {
		gone string
		call *weave.GNode
	}
	var broken []brokenCall
	for _, n := range g.Nodes {
		if n.Kind != weave.KindCall {
			continue
		}
		for _, r := range removed {
			if sameShape(r, n.Name) {
				broken = append(broken, brokenCall{gone: r, call: n})
				break
			}
		}
	}
	// The screens a removed endpoint served are the ones that called it before:
	// exact when the old graph is at hand, else every screen that reaches the caller.
	calledBy := map[string][]string{}
	if baseGraph != nil {
		for _, e := range baseGraph.Edges {
			if e.Kind == "calls" && strings.HasPrefix(e.From, "page:") && strings.HasPrefix(e.To, "endpoint:") {
				calledBy[strings.TrimPrefix(e.To, "endpoint:")] = append(calledBy[strings.TrimPrefix(e.To, "endpoint:")], strings.TrimPrefix(e.From, "page:"))
			}
		}
	}
	screensOf := map[string][]string{}
	for _, b := range broken {
		var screens []string
		if s, ok := calledBy[b.gone]; ok {
			screens = s
		} else if b.call.File != nil {
			file := *b.call.File
			if s, ok := screensOf[file]; ok {
				screens = s
			} else if fa := weave.Analyze(g, []string{file}); fa != nil {
				for _, p := range fa.Pages {
					screens = append(screens, p.Name)
				}
			}
			screensOf[file] = screens
		}
		res.Broken = append(res.Broken, Broken{b.gone, locNode(b.call), screens})
	}
	if len(broken) > 0 {
		var parts []string
		for i, b := range broken {
			if i == 4 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s from %s", b.gone, locNode(b.call)))
		}
		errors = append(errors, "removed endpoint still called: "+strings.Join(parts, "; "))
	}
	if len(removed) > 0 && len(broken) == 0 {
		shown := removed
		if len(shown) > 4 {
			shown = shown[:4]
		}
		warnings = append(warnings, fmt.Sprintf("removed %d endpoint(s), no live caller: %s", len(removed), strings.Join(shown, ", ")))
	}

	if f != nil {
		if len(f.Orphans) > 0 {
			errors = append(errors, fmt.Sprintf("call site(s) in changed files match no endpoint: %s", strings.Join(head(f.Orphans, 4), ", ")))
		}
		if len(f.Suspect) > 0 {
			warnings = append(warnings, fmt.Sprintf("requirement text changed, code not re-checked: %s", strings.Join(head(f.Suspect, 4), ", ")))
		}
		if len(f.Changes) == 0 {
			warnings = append(warnings, "no spec change record names these files")
		}
		if len(f.Untested) > 0 {
			msg := fmt.Sprintf("%d touched endpoint(s) have no direct test", len(f.Untested))
			if strict {
				errors = append(errors, msg)
			} else {
				warnings = append(warnings, msg)
			}
		}

		report, hasReport := junit.LoadReport(root)
		if hasReport && len(report.Cases) > 0 {
			if stale, newest := report.Stale(sourcePaths(root, f.Files)); stale {
				// A report older than the changed source is not evidence: neither a
				// failure nor a pass may be read out of it. The gate cannot judge the
				// requirement, so this is operational, not a green run.
				res.Operational = append(res.Operational, fmt.Sprintf(
					"JUnit report %s predates the changed source (newest %s) — evidence is stale; rerun the tests",
					relPath(root, report.Path), newest.Format("2006-01-02 15:04")))
			} else {
				ev := weave.EvidenceFacts(g, report.Cases, f.Reqs)
				var failing, unverified []weave.ReqEvidence
				for _, e := range ev {
					if e.Fail > 0 {
						failing = append(failing, e)
					}
					if e.Unverified {
						unverified = append(unverified, e)
					}
				}
				if len(failing) > 0 {
					var parts []string
					for i, e := range failing {
						if i == 3 {
							break
						}
						test := ""
						if len(e.Tests) > 0 {
							test = e.Tests[0]
						}
						parts = append(parts, fmt.Sprintf("%s (%s)", e.ID, test))
					}
					errors = append(errors, "requirement test failing: "+strings.Join(parts, "; "))
				}
				if len(unverified) > 0 {
					var ids []string
					for i, e := range unverified {
						if i == 4 {
							break
						}
						ids = append(ids, e.ID)
					}
					warnings = append(warnings, "implemented but never tested: "+strings.Join(ids, ", "))
				}
			}
		} else if len(f.Reqs) > 0 {
			warnings = append(warnings, fmt.Sprintf("%d requirement(s) linked but no JUnit report — evidence not checked", len(f.Reqs)))
		}
		res.Commands = f.Commands
		for _, p := range f.Pages {
			res.Pages = append(res.Pages, p.Name)
		}
		for _, e := range f.Endpoints {
			res.Endpoints = append(res.Endpoints, e.Name)
		}
	}

	scope := ""
	if f != nil {
		scope = fmt.Sprintf("%d file(s) → %d endpoint(s), %d page(s), %d test(s)", len(f.Files), len(f.Endpoints), len(f.Pages), len(f.Tests))
	} else {
		scope = fmt.Sprintf("%d endpoint(s) indexed, no changed file named", len(now))
	}
	extra := ""
	if against != "" {
		extra += " vs " + against
	}
	if len(removed) > 0 {
		extra += fmt.Sprintf(", %d endpoint(s) removed", len(removed))
	}
	res.Scope = scope + extra
	res.Errors = errors
	res.Warnings = warnings
	return res
}

// mergeBaseProblem reports why against and HEAD cannot be compared, or "" when
// they share history. `git merge-base` exits 1 when the histories are unrelated;
// that is the case a `against...HEAD` diff cannot express, so the gate must refuse
// rather than fall back to the local index or the named files alone.
func mergeBaseProblem(root, against string) string {
	cmd := exec.Command("git", "merge-base", against, "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err == nil {
		if strings.TrimSpace(string(out)) == "" {
			return "no merge base with HEAD (unrelated history)"
		}
		return ""
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if ee.ExitCode() == 1 {
			return "no merge base with HEAD (unrelated history)"
		}
		if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
			if line := strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0]); line != "" {
				return line
			}
		}
	}
	return err.Error()
}

// discoverChanges is the files a run covers. With an explicit ref it is the
// committed range at the merge base; without one it is the whole working tree,
// tracked and untracked. Output is NUL-separated so a path with spaces or other
// unusual characters survives intact instead of being quoted or split.
func discoverChanges(root, against string) ([]string, error) {
	if against != "" {
		// --relative keeps paths relative to root (and drops changes outside it) when
		// --root names a subdirectory, so they line up with the graph and the JUnit
		// freshness check.
		out, err := gitOut(root, "diff", "--name-only", "-z", "--no-ext-diff", "--relative", against+"...HEAD")
		if err != nil {
			return nil, fmt.Errorf("cannot read the %s change set: %w", against, err)
		}
		return cleanPaths(strings.Split(out, "\x00")), nil
	}
	out, err := gitOut(root, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("cannot read the working-tree change set: %w", err)
	}
	return cleanPaths(rootRelative(root, statusPaths(out))), nil
}

// rootRelative turns git's repo-root-relative porcelain paths into paths relative
// to root, so `--root <subdir>` matches the graph and the JUnit freshness check.
// Changes outside root are dropped: the gate scoped itself to root.
func rootRelative(root string, paths []string) []string {
	out, err := gitOut(root, "rev-parse", "--show-prefix")
	if err != nil {
		return paths
	}
	prefix := strings.TrimSpace(out)
	if prefix == "" {
		return paths
	}
	kept := paths[:0]
	for _, p := range paths {
		if strings.HasPrefix(p, prefix) {
			kept = append(kept, strings.TrimPrefix(p, prefix))
		}
	}
	return kept
}

// statusPaths reads porcelain v1 -z: `XY path\0` per entry, with a rename's
// original path as the next NUL entry after the new one.
func statusPaths(out string) []string {
	fields := strings.Split(out, "\x00")
	var files []string
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		xy := entry[:2]
		files = append(files, entry[3:])
		if xy[0] == 'R' || xy[0] == 'C' || xy[1] == 'R' || xy[1] == 'C' {
			i++ // skip the original path of a rename or copy
		}
	}
	return files
}

func cleanPaths(paths []string) []string {
	out := paths[:0]
	for _, p := range paths {
		if p == "" || strings.HasPrefix(p, ".weave/") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// gitOut runs git in root and keeps stderr, so an unreadable ref or a missing
// merge base says why instead of looking like an empty change set.
func gitOut(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
			// git prints usage after many fatal errors; the first line says what
			// went wrong and keeps a PR comment readable.
			if line := strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0]); line != "" {
				msg = line
			}
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
	}
	return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}

// gitMissing separates "no git installed" from "this is not a repo": the first
// must fail closed when the gate has to discover a change set, the second may use
// the last local index or explicit files.
func gitMissing() bool {
	_, err := exec.LookPath("git")
	return err != nil
}

// inWorkTree separates "git failed inside a repo" (operational) from "there is
// no repo here" (the explicit-files path still works).
func inWorkTree(root string) bool {
	out, err := gitOut(root, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func sourcePaths(root string, files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if filepath.IsAbs(f) {
			out = append(out, f)
			continue
		}
		out = append(out, filepath.Join(root, f))
	}
	return out
}

func relPath(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil {
		return rel
	}
	return p
}

func endpointNames(g *weave.Graph) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind == weave.KindEndpoint {
			out = append(out, n.Name)
		}
	}
	return out
}

// sameShape: call sites are relative to the mount, so compare tail segments.
// A parameter segment matches anything, so a match needs at least one literal
// segment in common: without that, `/stats` matched `users/:id` and a commit that
// removed an unrelated endpoint was reported as breaking a caller.
func sameShape(a, b string) bool {
	sa := strings.Fields(a)
	sb := strings.Fields(b)
	if len(sa) < 2 || len(sb) < 2 || sa[0] != sb[0] {
		return false
	}
	xa := segments(sa[1])
	xb := segments(sb[1])
	n := len(xa)
	if len(xb) < n {
		n = len(xb)
	}
	if n == 0 {
		return false
	}
	literal := false
	for i := 1; i <= n; i++ {
		p := xa[len(xa)-i]
		q := xb[len(xb)-i]
		param := strings.HasPrefix(p, ":") || strings.HasPrefix(q, ":") || p == "*" || q == "*"
		if param || p == q {
			if !param && p == q {
				literal = true
			}
			continue
		}
		return false
	}
	return literal
}

func segments(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func locNode(n *weave.GNode) string {
	file := "?"
	if n.File != nil {
		file = *n.File
	}
	if n.Line != nil && *n.Line > 0 {
		return fmt.Sprintf("%s:%d", file, *n.Line)
	}
	return file
}

func head[T any](list []T, n int) []T {
	if len(list) > n {
		return list[:n]
	}
	return list
}
