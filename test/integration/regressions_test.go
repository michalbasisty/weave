package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Shapes that tripped the gate on real repositories, kept as one small repo so a
// matcher change that brings a false FAIL back fails here first.
func TestRealRepoRegressions(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("backend/app.js", `const express = require('express'); const app = express();
app.put('/tasks/:id/start', (req, res) => res.json({}));
app.put('/tasks/:id/stop', (req, res) => res.json({}));
app.get('/articles/feed', (req, res) => res.json([]));
app.get('/articles/:slug', (req, res) => res.json({}));
`)
	// a literal union behind a template segment (2026-11, an Express + React app)
	write("web/src/types.ts", "export type TaskAction = 'start' | 'stop'\n")
	write("web/src/api.ts", `import axios from 'axios'
import type { TaskAction } from './types'
export const act = (id: string, action: TaskAction) => axios.put(`+"`/tasks/${id}/${action}`"+`)
export const article = (slug: string) => axios.get(`+"`/articles/${slug}`"+`)
export const feed = () => axios.get('/articles/feed')
`)
	// a generated client under a gitignored folder is not a module
	write(".gitignore", "web/src/generated/\n")
	write("web/src/generated/prisma/models/User.ts", "export const User = {}\nfetch('/users/me')\n")
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "init")

	// touching the caller must not fail: every call has a route
	write("web/src/api.ts", mustRead(t, filepath.Join(root, "web/src/api.ts"))+"// touched\n")
	if out, code := run(t, "check", "--root", root); code != 0 {
		t.Fatalf("false FAIL on a clean contract:\n%s", out)
	}
	if out, _ := run(t, "changed", "--root", root); strings.Contains(out, "generated") {
		t.Fatalf("gitignored folder reported as a change:\n%s", out)
	}

	// a union value with no route fails, names the exact path and the closest route
	write("web/src/types.ts", "export type TaskAction = 'start' | 'pause'\n")
	out, code := run(t, "check", "--root", root)
	if code != 1 || !strings.Contains(out, "PUT /tasks/${id}/pause") || !strings.Contains(out, "closest: PUT /tasks/:id/") {
		t.Fatalf("bad union value: exit %d\n%s", code, out)
	}
	write("web/src/types.ts", "export type TaskAction = 'start' | 'stop'\n")

	// renaming the param route is caught even though a fixed sibling sits beside it
	write("backend/app.js", strings.Replace(mustRead(t, filepath.Join(root, "backend/app.js")), "'/articles/:slug'", "'/posts/:slug'", 1))
	out, code = run(t, "check", "--root", root)
	if code != 1 || !strings.Contains(out, "removed endpoint still called: GET /articles/:slug") {
		t.Fatalf("renamed param route not caught: exit %d\n%s", code, out)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
