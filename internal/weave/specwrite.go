package weave

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"
)

// The change-record writers. The CLI, the panel and the MCP server run them, so a
// record has one implementation and one lock. Every operation goes through an
// os.Root pinned at the repository or at specs/, so a directory swapped for a link
// between the name check and the write cannot move the write outside the repo.

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
	dir := "specs/changes/" + slug
	repo, err := openRoot(root)
	if err != nil {
		return "", err
	}
	defer repo.Close()
	if err := repo.MkdirAll("specs/changes", 0o755); err != nil {
		return "", err
	}
	// Create the leaf exclusively. A directory, file, link or junction that appeared
	// between any earlier check and this write cannot be reused: the change record is
	// created once and never overwrites an existing one.
	if err := repo.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("%s already exists", dir)
		}
		return "", err
	}
	leaf, err := repo.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer leaf.Close()
	files := []struct{ name, body string }{
		{"proposal.md", "# Proposal: " + title + "\n\n## Why\n\n## What changes\n\n## Risks\n"},
		{"tasks.md", "# Tasks: " + title + "\n\n- [ ] T1 First task. Evidence: `path/to/file`\n"},
		{"spec-delta.md", "# Delta: " + title + "\n\n### ADDED REQ-" + strings.ToUpper(slugBase(slug)) + "-1 — " + title + "\n\nWHEN … THE system SHALL …\n"},
	}
	for _, f := range files {
		if err := leaf.WriteFile(f.name, []byte(f.body), 0o644); err != nil {
			return "", err
		}
	}
	return slug, nil
}

// DeleteSpecChange removes one change record and its three files. It is the only
// destructive writer, so the caller has to ask for it explicitly.
func DeleteSpecChange(root, change string) (string, error) {
	specs, err := openSpecsRoot(root)
	if err != nil {
		return "", err
	}
	defer specs.Close()
	name, err := findChange(specs, change)
	if err != nil {
		return "", err
	}
	if err := specs.RemoveAll("changes/" + name); err != nil {
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
	dir, name, err := changeRoot(root, change)
	if err != nil {
		return "", false, err
	}
	defer dir.Close()
	unlock, err := lockAt(dir, "tasks.md.lock")
	if err != nil {
		return name, false, err
	}
	defer unlock()

	// tasks.md must be a regular file, and the bytes must come from that file: the
	// handle is opened and checked once, so a link or a different file swapped in
	// after an earlier Lstat can neither supply the text this writer edits nor make
	// the read wait on a FIFO. A final link stays refused: a link is not the record
	// this writer owns, and replacing it would leave the change unreadable.
	f, err := openCheckedAt(dir, "tasks.md")
	if err != nil {
		return name, false, fmt.Errorf("specs/changes/%s/tasks.md: %w", name, err)
	}
	raw, err := io.ReadAll(f)
	_ = f.Close()
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
	if err := replaceFileAt(dir, "tasks.md", next); err != nil {
		return name, false, err
	}
	return name, true, nil
}

// SaveSpecText replaces one spec file. The lock keeps two editors from writing over
// each other and the temp file keeps a reader from seeing half a document. The
// parent directory is opened once as an os.Root, and the lock, temp file, chmod and
// rename all use that handle, so nothing is reopened by pathname after the check.
func SaveSpecText(root, rel, text string) error {
	parent, base, err := openSpecDir(root, rel)
	if err != nil {
		return err
	}
	defer parent.Close()
	return saveFileAt(parent, base, text)
}

// changeRoot pins one directory under specs/changes and returns its base name.
func changeRoot(root, change string) (*os.Root, string, error) {
	specs, err := openSpecsRoot(root)
	if err != nil {
		return nil, "", err
	}
	defer specs.Close()
	name, err := findChange(specs, change)
	if err != nil {
		return nil, "", err
	}
	dir, err := specs.OpenRoot("changes/" + name)
	if err != nil {
		return nil, "", err
	}
	return dir, name, nil
}

// errNotAChange says an entry exists but is not a change-record directory: a file,
// a symlink or a junction. The record writers refuse it rather than follow it.
var errNotAChange = errors.New("not a change record")

// checkChange reports whether rel is a real directory directly under
// specs/changes. A link is refused even when it points at another record: the
// writer must own the directory it edits, deletes or ticks. Errors other than the
// two sentinels are real traversal or I/O failures and are propagated.
func checkChange(specs *os.Root, rel string) error {
	info, err := specs.Lstat(rel)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: %s", errNotAChange, rel)
	}
	return nil
}

// findChange resolves a slug, or an unambiguous short name, to a real directory
// directly under specs/changes. The directory is read through the specs Root, so a
// name that is a link out of specs is resolved by os.Root and refused. A traversal
// error is reported as one, never as "no change".
func findChange(specs *os.Root, change string) (string, error) {
	if !specNameOK.MatchString(change) {
		return "", fmt.Errorf("change is a slug like 2026-10-22-billing-retry")
	}
	exact := "changes/" + change
	err := checkChange(specs, exact)
	if err == nil {
		return change, nil
	}
	if !errors.Is(err, errNotAChange) && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("cannot read %s: %w", exact, err)
	}
	entries, err := fs.ReadDir(specs.FS(), "changes")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("no change %q under specs/changes", change)
		}
		return "", fmt.Errorf("cannot read specs/changes: %w", err)
	}
	var matches []string
	for _, e := range entries {
		if !strings.Contains(e.Name(), change) {
			continue
		}
		rel := "changes/" + e.Name()
		cerr := checkChange(specs, rel)
		if cerr == nil {
			matches = append(matches, e.Name())
			continue
		}
		if errors.Is(cerr, errNotAChange) || errors.Is(cerr, fs.ErrNotExist) {
			continue
		}
		return "", fmt.Errorf("cannot read %s: %w", rel, cerr)
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("no change %q under specs/changes", change)
	}
	return matches[0], nil
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
