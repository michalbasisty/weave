package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michalbasisty/weave/internal/grep"
	"github.com/michalbasisty/weave/internal/junit"
	"github.com/michalbasisty/weave/internal/weave"
)

// The read surface: the same commands an agent gets over MCP, on the CLI.

func cmdContext() int {
	files := pos
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: context <file...> [--root <dir>] [--budget <tokens>]")
		return 2
	}
	depth := arg("depth", "names")
	if depth != "code" {
		depth = "names"
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	fmt.Println(weave.Context(g, files, numberArg("budget", "350"), depth))
	return 0
}

func cmdImpact() int {
	files := pos
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: impact <file...> [--root <dir>]")
		return 2
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	fmt.Println(weave.Impact(g, files))
	return 0
}

func cmdPlan() int {
	slug := ""
	if len(pos) > 0 {
		slug = pos[0]
	}
	if slug == "" {
		fmt.Fprintln(os.Stderr, "usage: plan <change-slug> [T3]")
		return 2
	}
	task := ""
	if len(pos) > 1 {
		task = pos[1]
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	fmt.Println(weave.Plan(g, slug, task))
	return 0
}

func cmdChange() int {
	slug := ""
	if len(pos) > 0 {
		slug = pos[0]
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	if slug != "" {
		fmt.Println(weave.ChangeView(g, slug))
	} else {
		fmt.Println(weave.ListChanges(g, 25))
	}
	return 0
}

func cmdSearch() int {
	q := strings.Join(pos, " ")
	if q == "" {
		fmt.Fprintln(os.Stderr, "usage: search <query> [--root <dir>]")
		return 2
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	fmt.Println(weave.Search(g, q, intArg("limit", "20")))
	return 0
}

func cmdGrep() int {
	q := strings.Join(pos, " ")
	if q == "" {
		fmt.Fprintln(os.Stderr, "usage: grep <text> [--limit N] [--regex]")
		return 2
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	fmt.Println(grep.Grep(g, q, intArg("limit", "20"), hasFlag("regex")))
	return 0
}

func cmdDoctor() int {
	g, code := graphFor()
	if g == nil {
		return code
	}
	fmt.Println(weave.Doctor(g))
	return 0
}

func cmdEvidence() int {
	report, ok := junit.LoadReport(root)
	if !ok || len(report.Cases) == 0 {
		fmt.Printf("no JUnit report found under %s\n  run your tests with a JUnit reporter, then retry (see: weave init)\n", root)
		return 0
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	if stale, newest := report.Stale(graphSources(g)); stale {
		fmt.Printf("evidence: JUnit report %s predates the newest source (%s) — evidence is stale; rerun the tests\n",
			relToRoot(report.Path), newest.Format("2006-01-02 15:04"))
		return 2
	}
	fmt.Println(weave.Evidence(g, report.Cases, pos))
	return 0
}

// graphSources is every source file the index knows, for the freshness check.
func graphSources(g *weave.Graph) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.Kind != weave.KindFile || n.Name == "" || seen[n.Name] {
			continue
		}
		seen[n.Name] = true
		if filepath.IsAbs(n.Name) {
			out = append(out, n.Name)
		} else {
			out = append(out, filepath.Join(root, n.Name))
		}
	}
	return out
}

func relToRoot(p string) string {
	if rel, err := filepath.Rel(root, p); err == nil {
		return rel
	}
	return p
}
