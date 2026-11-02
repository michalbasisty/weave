package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/baseline"
	"github.com/michalbasisty/weave/internal/delta"
	"github.com/michalbasisty/weave/internal/perf"
	"github.com/michalbasisty/weave/internal/registry"
	"github.com/michalbasisty/weave/internal/usage"
	"github.com/michalbasisty/weave/internal/weave"
)

func cmdProjects() int {
	fmt.Println(registry.Table(registry.Load()))
	return 0
}

func cmdForget() int {
	name := ""
	if len(pos) > 0 {
		name = pos[0]
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "usage: weave forget <name>")
		return 2
	}
	if registry.Forget(name) {
		fmt.Printf("forgot %s\n", name)
	} else {
		fmt.Printf("no project named %s\n", name)
	}
	return 0
}

func cmdPerf() int {
	runs := intArg("runs", "0")
	for i := 1; i <= runs; i++ {
		g, err := weave.Reindex(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		total := 0.0
		if g.Perf != nil {
			total = g.Perf.TotalMs
		}
		fmt.Printf("run %d: %s  %s\n", i, ms(total), weave.PhaseLine(g.Perf, true))
	}
	fmt.Println(perf.Report(perf.Read(root), 8))
	return 0
}

func ms(v float64) string { return fmt.Sprintf("%gms", v) }

func cmdRemember() int {
	note := arg("note", "")
	files := pos
	if note == "" || len(files) == 0 {
		fmt.Fprintln(os.Stderr, `usage: weave remember --note "what you learned" <file...>`)
		return 2
	}
	entry, err := weave.Remember(root, files, note, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	if err != nil || entry == nil {
		fmt.Println("nothing to remember")
		return 0
	}
	fmt.Printf("remembered for %d file(s): %s\n", len(entry.Files), entry.Note)
	return 0
}

func cmdRecall() int {
	text, err := weave.RecallText(root, pos)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(text)
	return 0
}

func cmdMemory() int {
	if hasFlag("clear") {
		n, err := weave.ClearMemory(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("memory cleared (%d note(s) dropped)\n", n)
		return 0
	}
	text, err := weave.RecallText(root, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(text)
	return 0
}

func cmdDelta() int {
	ref := "HEAD"
	if len(pos) > 0 {
		ref = pos[0]
	}
	from := arg("from", "")
	fromPath := ""
	if from != "" {
		fromPath = from
		if !filepath.IsAbs(fromPath) {
			fromPath = filepath.Join(root, fromPath)
		}
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	var base *weave.Graph
	name := ref
	if fromPath != "" {
		base = baseline.GraphFromFile(fromPath)
		name = filepath.Base(from)
	} else {
		base = baseline.GraphAt(root, ref)
	}
	if base == nil {
		if fromPath != "" {
			fmt.Fprintf(os.Stderr, "cannot read graph %s\n", fromPath)
		} else {
			fmt.Fprintf(os.Stderr, "cannot read %s — needs a git worktree (try --from <graph.json>)\n", ref)
		}
		return 1
	}
	fmt.Println(delta.Delta(g, base, name, hasFlag("all")))
	return 0
}

func cmdGain() int {
	fmt.Println(usage.Gain(root, intArg("since", "7")))
	return 0
}

func cmdAsk() int {
	q := strings.Join(pos, " ")
	if q == "" {
		fmt.Fprintln(os.Stderr, `usage: weave ask "question" [--file path]`)
		return 2
	}
	g, code := graphFor()
	if g == nil {
		return code
	}
	fmt.Println(weave.FormatAsk(weave.Ask(g, q, flagValues("file")), 18))
	return 0
}
