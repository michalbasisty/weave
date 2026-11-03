package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Editing specs is a file operation, not a browser one: `spec new` creates the
// three files a change needs, `spec task done` ticks a task without touching the
// rest of the line. The explorer stays read-only.

var datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`)

func cmdSpec() int {
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "usage: weave spec new <name> [--title …] | weave spec task done <change> <T1>")
		return 2
	}
	sub := pos[0]
	rest := pos[1:]
	switch sub {
	case "new":
		return specNew(rest)
	case "task":
		if len(rest) >= 1 && rest[0] == "done" {
			return specTaskDone(rest[1:])
		}
	}
	fmt.Fprintln(os.Stderr, "usage: weave spec new <name> [--title …] | weave spec task done <change> <T1>")
	return 2
}

func specNew(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: weave spec new <name> [--title …]")
		return 2
	}
	slug := args[0]
	if !datePrefix.MatchString(slug) {
		slug = time.Now().Format("2006-01-02") + "-" + slug
	}
	title := arg("title", strings.ReplaceAll(datePrefix.ReplaceAllString(slug, ""), "-", " "))
	if title == "" {
		title = slug
	}
	dir := filepath.Join(root, "specs", "changes", slug)
	if pathExists(dir) {
		fmt.Fprintf(os.Stderr, "%s already exists\n", filepath.Join("specs", "changes", slug))
		return 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	files := map[string]string{
		"proposal.md":   "# Proposal: " + title + "\n\n## Why\n\n## What changes\n\n## Risks\n",
		"tasks.md":      "# Tasks: " + title + "\n\n- [ ] T1 First task. Evidence: `path/to/file`\n",
		"spec-delta.md": "# Delta: " + title + "\n\n### ADDED REQ-" + strings.ToUpper(slugBase(slug)) + "-1 — " + title + "\n\nWHEN … THE system SHALL …\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	fmt.Printf("created specs/changes/%s\n  proposal.md\n  tasks.md\n  spec-delta.md\n", slug)
	fmt.Println("next: weave spec task done " + slug + " T1")
	return 0
}

// slugBase makes a requirement id from the slug: 2026-11-03-billing-retry -> BILLING_RETRY.
func slugBase(slug string) string {
	slug = datePrefix.ReplaceAllString(slug, "")
	var b strings.Builder
	for _, r := range slug {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r - 32)
		} else if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "APP"
	}
	return out
}

var taskLine = regexp.MustCompile(`(?m)^(\s*-\s*\[)([ xX])(\]\s*)([A-Za-z0-9.]+)(\b.*)$`)

func specTaskDone(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: weave spec task done <change> <T1>")
		return 2
	}
	change, task := args[0], args[1]
	dir := filepath.Join(root, "specs", "changes", change)
	if !pathExists(dir) {
		matches, _ := filepath.Glob(filepath.Join(root, "specs", "changes", "*"+change+"*"))
		if len(matches) == 1 {
			dir = matches[0]
		} else {
			fmt.Fprintf(os.Stderr, "no change %q under specs/changes\n", change)
			return 1
		}
	}
	path := filepath.Join(dir, "tasks.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	text := string(raw)
	found := false
	changed := taskLine.ReplaceAllStringFunc(text, func(line string) string {
		m := taskLine.FindStringSubmatch(line)
		if m == nil || m[4] != task {
			return line
		}
		found = true
		if m[2] == "x" || m[2] == "X" {
			return line
		}
		return m[1] + "x" + m[3] + m[4] + m[5]
	})
	if !found {
		fmt.Fprintf(os.Stderr, "no task %s in %s\n", task, filepath.Join("specs", "changes", filepath.Base(dir), "tasks.md"))
		return 1
	}
	if changed == text {
		fmt.Printf("%s %s is already done\n", filepath.Base(dir), task)
		return 0
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("done %s %s in specs/changes/%s/tasks.md\n", filepath.Base(dir), task, filepath.Base(dir))
	return 0
}
