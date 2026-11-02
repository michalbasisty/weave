package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/michalbasisty/weave/internal/grep"
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
	cases := loadCases()
	if len(cases) == 0 {
		fmt.Printf("no JUnit report found under %s\n  run your tests with a JUnit reporter, then retry (see: weave init)\n", root)
		return 0
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	fmt.Println(weave.Evidence(g, cases, pos))
	return 0
}
