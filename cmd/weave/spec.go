package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/server"
	"github.com/michalbasisty/weave/internal/weave"
)

// Editing specs is a file operation: `spec new` creates the three files a change
// needs, `spec task done` and `spec task reopen` change one checkbox without
// touching the rest of the line. The panel and the MCP server call the same
// functions, so every surface writes through one implementation.

func cmdSpec() int {
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, specUsage)
		return 2
	}
	sub := pos[0]
	rest := pos[1:]
	switch sub {
	case "new":
		return specNew(rest)
	case "rm":
		return specRemove(rest)
	case "list", "ls":
		return specList()
	case "show":
		return specShow(rest)
	case "save":
		return specSave(rest)
	case "task":
		if len(rest) >= 2 && (rest[0] == "done" || rest[0] == "reopen") {
			return specTaskSet(rest[1:], rest[0] == "done")
		}
	}
	fmt.Fprintln(os.Stderr, specUsage)
	return 2
}

const specUsage = "usage: weave spec new <name> [--title …] | weave spec rm <change> | weave spec task done|reopen <change> <T1>\n       weave spec list [--json] | weave spec show <specs/…md> [--json] | weave spec save <specs/…md> < text"

func specNew(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: weave spec new <name> [--title …]")
		return 2
	}
	slug, err := weave.NewSpecChange(root, args[0], arg("title", ""), time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("created specs/changes/%s\n  proposal.md\n  tasks.md\n  spec-delta.md\n", slug)
	fmt.Println("next: weave spec task done " + slug + " T1")
	return 0
}

// specRemove deletes one change record: the directory and its three files.
func specRemove(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: weave spec rm <change>")
		return 2
	}
	name, err := weave.DeleteSpecChange(root, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("deleted specs/changes/%s\n", name)
	return 0
}

// specTaskSet ticks or unticks a task: one checkbox, same line otherwise.
func specTaskSet(args []string, done bool) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: weave spec task done|reopen <change> <T1>")
		return 2
	}
	change, task := args[0], args[1]
	state, verb := "open", "reopened"
	if done {
		state, verb = "done", "done"
	}
	name, changed, err := weave.SetSpecTask(root, change, task, done)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !changed {
		fmt.Printf("%s %s is already %s\n", name, task, state)
		return 0
	}
	fmt.Printf("%s %s %s in specs/changes/%s/tasks.md\n", verb, name, task, name)
	return 0
}

// specList prints every change record with its tasks; --json prints what the
// panel's Specs drawer reads (changes, capabilities, screens), for other hosts.
func specList() int {
	g, err := weave.Reindex(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	payload := server.SpecsPayload(root, g)
	if hasFlag("json") {
		return printJSON(payload)
	}
	b, _ := json.Marshal(payload)
	var data struct {
		Changes []struct {
			Slug        string
			Done, Total int
			Tasks       []struct {
				ID, Title string
				Done      bool
			}
		}
		Capabilities []struct {
			Name, File  string
			Parts, Reqs int
		}
	}
	_ = json.Unmarshal(b, &data)
	if len(data.Changes) == 0 && len(data.Capabilities) == 0 {
		fmt.Println("no specs yet: weave spec new <name> starts a change record")
		return 0
	}
	for _, c := range data.Changes {
		fmt.Printf("%s  %d/%d\n", c.Slug, c.Done, c.Total)
		for _, t := range c.Tasks {
			box := "[ ]"
			if t.Done {
				box = "[x]"
			}
			fmt.Printf("  %s %s %s\n", box, t.ID, t.Title)
		}
	}
	for _, c := range data.Capabilities {
		fmt.Printf("capability %s  %s  %d part(s), %d requirement(s)\n", c.Name, c.File, c.Parts, c.Reqs)
	}
	return 0
}

// specShow prints one spec file; --json adds its title and HTML rendered for people.
func specShow(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: weave spec show <specs/…md> [--json]")
		return 2
	}
	page, err := server.SpecPage(root, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if hasFlag("json") {
		return printJSON(page)
	}
	fmt.Print(page["text"])
	return 0
}

// specSave replaces one spec file with standard input, through the same writer
// as the panel's editor.
func specSave(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: weave spec save <specs/…md> < text")
		return 2
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 512*1024+1))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if strings.TrimSpace(string(raw)) == "" {
		fmt.Fprintln(os.Stderr, "weave spec save: refusing to write an empty spec")
		return 1
	}
	rel, err := server.SaveSpec(root, args[0], string(raw))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("saved %s (%d bytes)\n", rel, len(raw))
	return 0
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
