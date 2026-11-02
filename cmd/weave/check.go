package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/michalbasisty/weave/internal/baseline"
	"github.com/michalbasisty/weave/internal/weave"
)

var refSanitise = regexp.MustCompile(`[^\w./~^-]`)
var gitPrefix = regexp.MustCompile(`^[AMDR?]{1,2}\s+`)

func gitChanged(root, against string) []string {
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	rangeSpec := "HEAD"
	if against != "" {
		rangeSpec = against + "...HEAD"
	}
	out := run("diff", "--name-only", rangeSpec)
	if out == "" && against == "" {
		out = run("status", "--porcelain")
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = gitPrefix.ReplaceAllString(line, "")
		if line != "" && !strings.HasPrefix(line, ".weave/") {
			files = append(files, line)
		}
	}
	return files
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

func cmdCheck() int {
	strict := hasFlag("strict-tests")
	against := refSanitise.ReplaceAllString(arg("against", ""), "")

	g, err := weave.Reindex(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	files := pos
	if len(files) == 0 {
		files = gitChanged(root, against)
	}
	var f *weave.Facts
	if len(files) > 0 {
		f = weave.Analyze(g, files)
	}

	var errors, warnings []string

	var prev []string
	if against != "" {
		if base := baseline.GraphAt(root, against); base != nil {
			prev = endpointNames(base)
		} else {
			warnings = append(warnings, fmt.Sprintf("could not read %s — comparing against the last local index instead", against))
			prev = g.PrevEndpoints
		}
	} else {
		prev = g.PrevEndpoints
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

		cases := loadCases()
		if len(cases) > 0 {
			ev := weave.EvidenceFacts(g, cases, f.Reqs)
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
		} else if len(f.Reqs) > 0 {
			warnings = append(warnings, fmt.Sprintf("%d requirement(s) linked but no JUnit report — evidence not checked", len(f.Reqs)))
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
	fmt.Printf("check: %s%s\n", scope, extra)
	for _, e := range errors {
		fmt.Printf("  FAIL %s\n", e)
	}
	for _, w := range warnings {
		fmt.Printf("  warn %s\n", w)
	}
	if f != nil {
		for _, c := range f.Commands {
			fmt.Printf("  run  %s\n", c)
		}
	}
	if len(errors) > 0 {
		return 1
	}
	return 0
}

// sameShape: call sites are relative to the mount, so compare tail segments.
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
	for i := 1; i <= n; i++ {
		p := xa[len(xa)-i]
		q := xb[len(xb)-i]
		if strings.HasPrefix(p, ":") || strings.HasPrefix(q, ":") || p == "*" || q == "*" || p == q {
			continue
		}
		return false
	}
	return true
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
