// Command weave is the Go implementation of the weave CLI and MCP server.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/michalbasisty/weave/internal/registry"
	"github.com/michalbasisty/weave/internal/version"
	"github.com/michalbasisty/weave/internal/weave"
)

func main() {
	cmd, pos = parseArgs(os.Args[1:])
	r, code := pickRoot()
	if code != 0 {
		os.Exit(code)
	}
	root = r
	if isProject(root) {
		registry.Touch(root)
	}

	switch cmd {
	case "index":
		code = cmdIndex()
	case "explore":
		code = cmdExplore()
	case "wiki":
		code = cmdWiki()
	case "serve":
		code = cmdServe()
	case "projects", "list":
		code = cmdProjects()
	case "forget":
		code = cmdForget()
	case "context", "brief":
		code = cmdContext()
	case "impact":
		code = cmdImpact()
	case "plan":
		code = cmdPlan()
	case "change":
		code = cmdChange()
	case "search":
		code = cmdSearch()
	case "grep":
		code = cmdGrep()
	case "remember":
		code = cmdRemember()
	case "recall":
		code = cmdRecall()
	case "memory":
		code = cmdMemory()
	case "doctor", "status":
		code = cmdDoctor()
	case "perf":
		code = cmdPerf()
	case "check":
		code = cmdCheck()
	case "hook":
		code = cmdHook()
	case "fires":
		code = cmdFires()
	case "delta":
		code = cmdDelta()
	case "gain":
		code = cmdGain()
	case "evidence":
		code = cmdEvidence()
	case "ask":
		code = cmdAsk()
	case "spec":
		code = cmdSpec()
	case "init":
		code = cmdInit()
	case "mcp":
		code = cmdMcp()
	case "help", "--help", "-h":
		fmt.Println(helpText())
	case "version", "--version", "-v":
		fmt.Printf("weave %s\n", version.Version)
	default:
		home()
	}
	os.Exit(code)
}

func cmdIndex() int {
	g, err := weave.Reindex(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var kinds []string
	counts := map[string]int{}
	for _, n := range g.Nodes {
		if counts[n.Kind] == 0 {
			kinds = append(kinds, n.Kind)
		}
		counts[n.Kind]++
	}
	var parts []string
	for _, k := range kinds {
		if k != weave.KindFile {
			parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
		}
	}
	fmt.Printf("indexed %d files, %d nodes, %d edges in %s\n", g.FileCount, len(g.Nodes), len(g.Edges), elapsed(g))
	fmt.Println(joinParts(parts))
	if g.Perf != nil {
		fmt.Println(weave.PhaseLine(g.Perf, false))
	}
	fmt.Println(weave.IndexPath(root))
	// A wiki that exists is kept current: the local site is never a claim about
	// yesterday's tree. Committed sites (docs/wiki) are explicit: `weave wiki --out`.
	localWiki := filepath.Join(root, ".weave", "wiki")
	if weave.ReadWikiStamp(localWiki) != "" || pathExists(filepath.Join(localWiki, "index.html")) {
		if res, err := weave.WriteWiki(g, weave.WikiOptions{OutDir: localWiki, Commit: gitHead(root)}); err == nil {
			fmt.Printf("wiki: %d page(s) refreshed in %s\n", res.Pages, localWiki)
		}
	}
	return 0
}

func helpText() string {
	return `weave — repo map for agents and people

  weave                         status, and what to do next
  weave explore                 open the map in your browser (--all: every project)
  weave wiki                    write a read-only wiki: a page per module, endpoint, screen, table, change
  weave wiki --check            fail when the wiki no longer describes HEAD
  weave projects                every repo weave knows, and where they are
  weave context <file...>       one file, explained (--budget tokens, --depth code)
  weave impact  <file...>       full blast radius
  weave plan    <change> [T3]   the next task, its files, its command
  weave spec    new <name>      create proposal.md, tasks.md, spec-delta.md
  weave spec    task done <change> <T1>, task reopen <change> <T1>
  weave spec    rm <change>        delete a change record (destructive)
  weave ask     "question"      a cheap model answers from the graph
  weave change  [slug]          spec change records
  weave search  <query>
  weave grep    <text>          matching source lines (--regex, --limit)
  weave remember --note "…" <file...>   leave a note for the next session
  weave recall  [file...]       notes for these files
  weave memory                  all notes (--prune drops ownership notes and repeats, --clear drops all)
  weave check   [file...]       fail when a change breaks a contract (no args: git diff)
  weave hook                    check for an agent hook: exit 2 and the failure on stderr
  weave serve   [--port N]      host the map, the wiki and the actions on 127.0.0.1, and open it
                [--no-open]     don't open the browser
  weave fires   [--since N]     what the hook has seen: runs, failures, avg time
  weave delta   [ref]           what the graph gained or lost (--from graph.json, --all)
  weave gain    [--since N]     tokens served over the last N days (default 7)
  weave evidence [REQ...]       which requirements a JUnit report proves
  weave doctor                  graph health
  weave perf    [--runs N]      index timings over time, or N fresh runs
  weave init    [--upgrade]     connect Claude, Cursor, VS Code, opencode
  weave init    --only claude   write only the configs you name (claude,cursor,vscode,opencode)
  weave init    --hooks         also run the gate when the agent stops (Claude Code, opencode)
  weave init    --no-instructions  leave CLAUDE.md and AGENTS.md alone
  weave forget  <name>          drop a project from the list
  weave mcp                     MCP server on stdio (--all: serve every project)
  weave --version               the release version

--root <dir>        point at a specific checkout
--project <name>    use a registered project from anywhere
--budget <tokens>   context size (default 350)
--depth code        context: a short window at each anchor (opt-in; never whole files)
--out <file>        explore output path (default .weave/explore.html)
--since <ref>       explore: what changed since this git ref (default HEAD; --no-delta skips it)`
}

func home() {
	if !hasFlag("root") && !isProject(root) {
		fmt.Printf(`%s
  not a project yet — no .git, .weave or package.json here.

  cd into a repository, or:  weave --root <dir>
`, root)
		return
	}
	g, code := graphFor()
	if g == nil {
		os.Exit(code)
	}
	count := func(kind string) int {
		n := 0
		for _, x := range g.Nodes {
			if x.Kind == kind {
				n++
			}
		}
		return n
	}
	fmt.Println(g.Root)
	fmt.Printf("  %d files · %d endpoints · %d pages · %d tables · %d change records\n",
		g.FileCount, count(weave.KindEndpoint), count(weave.KindPage), count(weave.KindTable), count(weave.KindChange))
	others := len(registry.Load()) - 1
	if others < 0 {
		others = 0
	}
	extra := "your projects, all in one list"
	if others > 0 {
		extra = fmt.Sprintf("%d other project(s) — switch with --project <name>", others)
	}
	fmt.Printf(`
  weave explore          open the map
  weave plan <slug>      the next task of a change
  weave check            what breaks (exit 1)
  weave evidence         what your tests prove
  weave context <file>   one file, explained
  weave projects         %s
  weave init             connect your agent

  weave --help           all commands
  weave --version        the release version
`, extra)
}
