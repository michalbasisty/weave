package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

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
	case "task":
		if len(rest) >= 2 && (rest[0] == "done" || rest[0] == "reopen") {
			return specTaskSet(rest[1:], rest[0] == "done")
		}
	case "unlock":
		return specUnlock(rest)
	}
	fmt.Fprintln(os.Stderr, specUsage)
	return 2
}

const specUsage = "usage: weave spec new <name> [--title …] | weave spec rm <change> | weave spec task done|reopen <change> <T1> | weave spec unlock <spec-relative.md> --confirm-stopped"

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

// specUnlock removes one legacy lock marker as an explicit operator action. It
// never runs automatically, cannot prove the marker's owner is dead, and refuses
// to touch anything without --confirm-stopped after the operator has stopped every
// writer.
func specUnlock(args []string) int {
	confirmed := hasFlag("confirm-stopped")
	rel := ""
	for _, a := range args {
		if a == "--confirm-stopped" {
			confirmed = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(os.Stderr, "unknown option %s\n", a)
			return 2
		}
		if rel != "" {
			fmt.Fprintln(os.Stderr, "usage: weave spec unlock <spec-relative.md> --confirm-stopped")
			return 2
		}
		rel = a
	}
	if rel == "" {
		fmt.Fprintln(os.Stderr, "usage: weave spec unlock <spec-relative.md> --confirm-stopped")
		return 2
	}
	fmt.Fprintln(os.Stderr, weave.SpecUnlockWarning)
	if !confirmed {
		fmt.Fprintln(os.Stderr, "refusing: stop every writer, then pass --confirm-stopped")
		return 2
	}
	lock, err := weave.UnlockSpecLock(root, rel)
	if errors.Is(err, weave.ErrNoLockMarker) {
		fmt.Printf("no lock marker at %s; nothing to remove\n", lock)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("removed %s\n", lock)
	return 0
}
