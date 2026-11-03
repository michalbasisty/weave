package runners

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func check(t *testing.T, got RunPlan, commands, files []string) {
	t.Helper()
	if !slices.Equal(got.Commands, commands) {
		t.Errorf("commands = %q, want %q", got.Commands, commands)
	}
	if !slices.Equal(got.Files, files) {
		t.Errorf("files = %q, want %q", got.Files, files)
	}
}

func TestPlanVitestGroupsFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"devDependencies":{"vitest":"^1"}}`)
	check(t, Plan(root, []string{"src/a.test.ts", "src/b.test.ts"}),
		[]string{"npx vitest run src/a.test.ts src/b.test.ts"}, []string{})
}

// One test file reaches several endpoints; the command must not repeat it.
func TestPlanQuotesARepeatedFileOnce(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"devDependencies":{"vitest":"^1"}}`)
	check(t, Plan(root, []string{"src/a.test.ts", "src/a.test.ts", "src/b.test.ts"}),
		[]string{"npx vitest run src/a.test.ts src/b.test.ts"}, []string{})
}

func TestPlanJestConfigBeatsViteAndPlaywrightWinsE2E(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"devDependencies":{"vitest":"^1","jest":"^29","@playwright/test":"^1"}}`)
	write(t, root, "jest.config.js", "export default {}")
	write(t, root, "vite.config.ts", "")
	check(t, Plan(root, []string{"src/a.test.ts", "e2e/login.spec.ts"}),
		[]string{"npx jest src/a.test.ts", "npx playwright test e2e/login.spec.ts"}, []string{})
}

func TestPlanGoModule(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module x")
	check(t, Plan(root, []string{"internal/a/x_test.go", "internal/b/y_test.go", "main_test.go"}),
		[]string{"go test ./internal/a ./internal/b/..."}, []string{})
}

func TestPlanPHPUnitAndUngrouped(t *testing.T) {
	t.Run("phpunit", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "phpunit.xml", "<testsuites/>")
		check(t, Plan(root, []string{"tests/A.php"}),
			[]string{"vendor/bin/phpunit tests/A.php"}, []string{})
	})
	t.Run("no package", func(t *testing.T) {
		root := t.TempDir()
		check(t, Plan(root, []string{"src/a.test.ts", "b.test.ts"}),
			[]string{}, []string{"src/a.test.ts", "b.test.ts"})
	})
}

func TestPlanNestedDirectoryAndFourCommandCap(t *testing.T) {
	root := t.TempDir()
	write(t, root, "packages/web/package.json", `{"dependencies":{"vitest":"^1"}}`)
	pkgJSON := `{"devDependencies":{"vitest":"^1"}}`
	for _, dir := range []string{"p0", "p1", "p2", "p3", "p4"} {
		write(t, root, dir+"/package.json", pkgJSON)
	}
	got := Plan(root, []string{
		"packages/web/src/a.test.ts",
		"p0/a.test.ts", "p1/a.test.ts", "p2/a.test.ts", "p3/a.test.ts", "p4/a.test.ts",
	})
	want := []string{
		"cd packages/web && npx vitest run src/a.test.ts",
		"cd p0 && npx vitest run a.test.ts",
		"cd p1 && npx vitest run a.test.ts",
		"cd p2 && npx vitest run a.test.ts",
	}
	if !slices.Equal(got.Commands, want) {
		t.Errorf("commands = %q, want %q", got.Commands, want)
	}
}

func TestPlanScriptFallbacks(t *testing.T) {
	for _, c := range []struct{ script, want string }{
		{"vitest run", "npx vitest run a.test.ts"},
		{"jest --ci", "npx jest a.test.ts"},
	} {
		root := t.TempDir()
		write(t, root, "package.json", `{"scripts":{"test":"`+c.script+`"}}`)
		check(t, Plan(root, []string{"a.test.ts"}), []string{c.want}, []string{})
	}
}
