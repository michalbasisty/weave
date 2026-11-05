package weave

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The change-record writers. The CLI, the panel and the MCP server run them, so a
// record has one implementation and one lock.

var datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`)

// NewSpecChange creates specs/changes/<slug> and the three files a change needs. A
// name without a date prefix gets today's, so callers can pass either form.
func NewSpecChange(root, name, title string, now time.Time) (string, error) {
	if !specNameOK.MatchString(name) {
		return "", fmt.Errorf("a name is letters, digits, dash or underscore: billing-retry")
	}
	slug := name
	if !datePrefix.MatchString(slug) {
		slug = now.Format("2006-01-02") + "-" + slug
	}
	if title == "" {
		title = strings.ReplaceAll(datePrefix.ReplaceAllString(slug, ""), "-", " ")
	}
	if title == "" {
		title = slug
	}
	dir := filepath.Join(root, "specs", "changes", slug)
	if isDir(dir) {
		return "", fmt.Errorf("%s already exists", filepath.Join("specs", "changes", slug))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	files := []struct{ name, body string }{
		{"proposal.md", "# Proposal: " + title + "\n\n## Why\n\n## What changes\n\n## Risks\n"},
		{"tasks.md", "# Tasks: " + title + "\n\n- [ ] T1 First task. Evidence: `path/to/file`\n"},
		{"spec-delta.md", "# Delta: " + title + "\n\n### ADDED REQ-" + strings.ToUpper(slugBase(slug)) + "-1 — " + title + "\n\nWHEN … THE system SHALL …\n"},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.body), 0o644); err != nil {
			return "", err
		}
	}
	return slug, nil
}

// DeleteSpecChange removes one change record and its three files. It is the only
// destructive writer, so the caller has to ask for it explicitly.
func DeleteSpecChange(root, change string) (string, error) {
	dir, name, err := changeDir(root, change)
	if err != nil {
		return "", err
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	return name, nil
}

// SetSpecTask ticks or unticks one task in specs/changes/<change>/tasks.md and
// leaves every other byte alone. The returned name is the change directory, which
// is the slug a later call needs. A caller renders "already done" when changed is
// false.
func SetSpecTask(root, change, task string, done bool) (name string, changed bool, err error) {
	if !specTaskOK.MatchString(task) {
		return "", false, fmt.Errorf("task is an id like T1")
	}
	dir, name, err := changeDir(root, change)
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(dir, "tasks.md")
	unlock, err := lockFile(path)
	if err != nil {
		return name, false, err
	}
	defer unlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		return name, false, err
	}
	text := string(raw)
	found := false
	next := specTaskLine.ReplaceAllStringFunc(text, func(line string) string {
		m := specTaskLine.FindStringSubmatch(line)
		if m == nil || m[4] != task {
			return line
		}
		found = true
		if (m[2] == "x" || m[2] == "X") == done {
			return line
		}
		mark := " "
		if done {
			mark = "x"
		}
		return m[1] + mark + m[3] + m[4] + m[5]
	})
	if !found {
		return name, false, fmt.Errorf("no task %s in specs/changes/%s/tasks.md", task, name)
	}
	if next == text {
		return name, false, nil
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return name, false, err
	}
	return name, true, nil
}

// changeDir resolves a slug, or an unambiguous short name, to a directory directly
// under specs/changes and returns its base name.
func changeDir(root, change string) (string, string, error) {
	if !specNameOK.MatchString(change) {
		return "", "", fmt.Errorf("change is a slug like 2026-10-22-billing-retry")
	}
	dir := filepath.Join(root, "specs", "changes", change)
	if !isDir(dir) {
		matches, _ := filepath.Glob(filepath.Join(root, "specs", "changes", "*"+change+"*"))
		if len(matches) != 1 || !isDir(matches[0]) {
			return "", "", fmt.Errorf("no change %q under specs/changes", change)
		}
		dir = matches[0]
	}
	return dir, filepath.Base(dir), nil
}

// lockFile keeps two writers from reading the same tasks.md and each writing the
// other's tick away. The lock is a file created with O_EXCL, removed on release; a
// lock older than 5s is stale, because a crashed writer leaves its file behind.
func lockFile(path string) (func(), error) {
	lock := path + ".lock"
	deadline := time.Now().Add(2 * time.Second)
	var last error
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lock) }, nil
		}
		last = err
		if errors.Is(err, fs.ErrExist) {
			// A crashed writer leaves its file behind; a lock older than 5s is stale.
			if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > 5*time.Second {
				_ = os.Remove(lock)
				continue
			}
		}
		// On Windows a lock that is being removed surfaces as access denied, so every
		// error is retried until the deadline rather than failing the write.
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("tasks.md is locked by another writer: %w", last)
		}
		time.Sleep(20 * time.Millisecond)
	}
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

var (
	specTaskLine = regexp.MustCompile(`(?m)^(\s*-\s*\[)([ xX])(\]\s*)([A-Za-z0-9.]+)(\b.*)$`)
	specNameOK   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	specTaskOK   = regexp.MustCompile(`^[A-Za-z0-9.]+$`)
)

func isDir(p string) bool {
	s, err := os.Stat(p)
	return err == nil && s.IsDir()
}
