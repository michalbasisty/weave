package action_test

// The SARIF upload is opt-in, so its gate is small enough to execute: the Check
// step is extracted from action.yml and run with a stub weave binary. These
// tests pin the default (off), the exit-code gate (0/1 upload, 2 never), the
// upload preconditions (against, root, fork) and the outputs the upload step
// and callers read.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type actionFile struct {
	Inputs map[string]struct {
		Description string `yaml:"description"`
		Required    bool   `yaml:"required"`
		Default     string `yaml:"default"`
	} `yaml:"inputs"`
	Outputs map[string]struct {
		Description string `yaml:"description"`
		Value       string `yaml:"value"`
	} `yaml:"outputs"`
	Runs struct {
		Using string       `yaml:"using"`
		Steps []actionStep `yaml:"steps"`
	} `yaml:"runs"`
}

type actionStep struct {
	Name            string            `yaml:"name"`
	ID              string            `yaml:"id"`
	If              string            `yaml:"if"`
	Uses            string            `yaml:"uses"`
	Shell           string            `yaml:"shell"`
	ContinueOnError bool              `yaml:"continue-on-error"`
	Env             map[string]string `yaml:"env"`
	With            map[string]string `yaml:"with"`
	Run             string            `yaml:"run"`
}

func loadAction(t *testing.T) actionFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var act actionFile
	if err := yaml.Unmarshal(raw, &act); err != nil {
		t.Fatalf("action.yml: %v", err)
	}
	if act.Runs.Using != "composite" {
		t.Fatalf("runs.using = %q, want composite", act.Runs.Using)
	}
	return act
}

func stepByID(t *testing.T, act actionFile, id string) actionStep {
	t.Helper()
	for _, s := range act.Runs.Steps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("action.yml has no step with id %q", id)
	return actionStep{}
}

func stepByUses(t *testing.T, act actionFile, uses string) actionStep {
	t.Helper()
	for _, s := range act.Runs.Steps {
		if s.Uses == uses {
			return s
		}
	}
	t.Fatalf("action.yml has no step using %q", uses)
	return actionStep{}
}

func stepByName(t *testing.T, act actionFile, name string) actionStep {
	t.Helper()
	for _, s := range act.Runs.Steps {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("action.yml has no step named %q", name)
	return actionStep{}
}

func stepIndex(t *testing.T, act actionFile, name string) int {
	t.Helper()
	for i, s := range act.Runs.Steps {
		if s.Name == name {
			return i
		}
	}
	t.Fatalf("action.yml has no step named %q", name)
	return -1
}

// The action must declare the feature off by default, wire the report to an
// output, use the pinned uploader and keep every input out of shell text.
func TestActionDeclaresOptInSARIFUpload(t *testing.T) {
	act := loadAction(t)

	in, ok := act.Inputs["sarif"]
	if !ok {
		t.Fatal("action.yml has no sarif input")
	}
	if in.Default != "false" {
		t.Fatalf("sarif default = %q, want false", in.Default)
	}
	for _, want := range []string{"security-events: write", "against", "root", "fork", "exit 2", "warning", "continue-on-error", "never masks", "twice", "empty/invalid"} {
		if !strings.Contains(in.Description, want) {
			t.Errorf("sarif description does not mention %q: %s", want, in.Description)
		}
	}

	out, ok := act.Outputs["sarif-file"]
	if !ok {
		t.Fatal("action.yml has no sarif-file output")
	}
	if !strings.Contains(out.Value, "steps.check.outputs.sarif") {
		t.Fatalf("sarif-file output = %q", out.Value)
	}

	check := stepByID(t, act, "check")
	if check.Shell != "bash" {
		t.Fatalf("check shell = %q", check.Shell)
	}
	for _, env := range []string{"ROOT", "AGAINST", "STRICT_TESTS", "SARIF", "FORK"} {
		if _, ok := check.Env[env]; !ok {
			t.Errorf("check step does not pass %s through env", env)
		}
	}
	for _, want := range []string{"--format sarif", "operational", "rc=", "sarif="} {
		if !strings.Contains(check.Run, want) {
			t.Errorf("check step script does not handle %q", want)
		}
	}

	upload := stepByUses(t, act, "github/codeql-action/upload-sarif@v4")
	if !strings.Contains(upload.If, "steps.check.outputs.sarif != ''") {
		t.Fatalf("upload condition = %q", upload.If)
	}
	if got := upload.With["sarif_file"]; got != "${{ steps.check.outputs.sarif }}" {
		t.Fatalf("upload sarif_file = %q", got)
	}
	if !upload.ContinueOnError {
		t.Fatal("upload must be continue-on-error so a failed upload cannot mask the gate")
	}
	if upload.ID == "" {
		t.Fatal("upload needs an id so the warning step can read its outcome")
	}

	// User-controlled input reaches a script only through env. Inline ${{ inputs.* }}
	// in a run block is command injection waiting for a quote.
	for _, s := range act.Runs.Steps {
		if strings.Contains(s.Run, "${{ inputs.") {
			t.Errorf("step %q interpolates an input into shell text", s.Name)
		}
	}
}

// A failed upload is a warning, not the verdict. The upload continues on error,
// the warning is executable in isolation, and the comment and both failing gate
// steps stay keyed to the check outputs rather than the upload outcome.
func TestSARIFUploadFailureOnlyWarns(t *testing.T) {
	act := loadAction(t)
	upload := stepByUses(t, act, "github/codeql-action/upload-sarif@v4")
	if !upload.ContinueOnError {
		t.Fatal("upload is not continue-on-error")
	}
	if upload.ID == "" {
		t.Fatal("upload has no id")
	}

	warnName := "Warn when the SARIF upload failed"
	warn := stepByName(t, act, warnName)
	wantIf := "steps." + upload.ID + ".outcome == 'failure'"
	if !strings.Contains(warn.If, wantIf) {
		t.Fatalf("warning condition = %q, want %q", warn.If, wantIf)
	}
	if warn.Shell != "bash" {
		t.Fatalf("warning shell = %q", warn.Shell)
	}
	if !strings.Contains(warn.Run, "::warning::") || !strings.Contains(warn.Run, "security-events: write") {
		t.Fatalf("warning script = %q", warn.Run)
	}

	uploadIndex := stepIndex(t, act, upload.Name)
	if got := stepIndex(t, act, warnName); got != uploadIndex+1 {
		t.Fatalf("warning step index = %d, want %d (immediately after the upload)", got, uploadIndex+1)
	}

	// Execute the warning step itself: it must surface the failure and exit 0.
	shell := bashPath(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "warn.sh"), []byte(warn.Run), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shell, "./warn.sh")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("warning step must not fail the job, got %v:\n%s", err, out)
	}
	if !strings.Contains(string(out), "::warning::") || !strings.Contains(string(out), "security-events: write") {
		t.Fatalf("warning output = %q", out)
	}

	// The steps that carry the verdict must not depend on the upload at all.
	for _, name := range []string{"Comment on the pull request", "Fail when the gate could not run", "Fail on problems"} {
		s := stepByName(t, act, name)
		if strings.Contains(s.If, "steps."+upload.ID) || strings.Contains(strings.ToLower(s.If), "upload") {
			t.Errorf("step %q is gated on the upload: %q", name, s.If)
		}
		if stepIndex(t, act, name) < uploadIndex {
			t.Errorf("step %q runs before the upload", name)
		}
	}
}

// The Check step decides whether a report may be uploaded. Its outputs are the
// whole gate: an empty sarif output skips the upload step, while only exit 0/1
// with a nonempty, valid report at an equivalent root produces one. Exit 2,
// empty or invalid output, parent paths and absolute roots must never produce one.
func TestCheckStepSARIFGateAndOutputs(t *testing.T) {
	act := loadAction(t)
	check := stepByID(t, act, "check")
	shell := bashPath(t)

	cases := []struct {
		name            string
		root            string
		against         string
		fork            string
		sarif           string
		rc              int
		strict          bool
		reaches         bool
		sarifMode       string
		sarifRC         string
		wantFailed      string
		wantOperational string
		wantReaches     string
		wantSarif       bool
		wantWarning     string
	}{
		{
			name: "default off", root: ".", against: "origin/main", sarif: "false",
			wantFailed: "false", wantOperational: "false", wantReaches: "false",
		},
		{
			name: "enabled on a pass", root: ".", against: "origin/main", sarif: "true",
			wantFailed: "false", wantOperational: "false", wantReaches: "false", wantSarif: true,
		},
		{
			name: "enabled on a contract failure", root: ".", against: "origin/main", sarif: "true", rc: 1, reaches: true,
			wantFailed: "true", wantOperational: "false", wantReaches: "true", wantSarif: true,
		},
		{
			name: "enabled operational exit 2", root: ".", against: "origin/main", sarif: "true", rc: 2,
			wantFailed: "false", wantOperational: "true", wantReaches: "false",
			wantWarning: "the gate could not run",
		},
		{
			name: "enabled without against", root: ".", against: "", sarif: "true",
			wantFailed: "false", wantOperational: "false", wantReaches: "false",
			wantWarning: "against must name a git ref",
		},
		{
			name: "enabled with another root", root: "test/fixture", against: "origin/main", sarif: "true",
			wantFailed: "false", wantOperational: "false", wantReaches: "false",
			wantWarning: "root must be '.'",
		},
		{
			name: "enabled on a fork pull request", root: ".", against: "origin/main", fork: "true", sarif: "true",
			wantFailed: "false", wantOperational: "false", wantReaches: "false",
			wantWarning: "read-only token",
		},
		{
			name: "enabled with an equivalent root", root: "./", against: "origin/main", sarif: "true",
			wantFailed: "false", wantOperational: "false", wantReaches: "false", wantSarif: true,
		},
		{
			name: "enabled with a normalized equivalent root", root: "././", against: "origin/main", sarif: "true",
			wantFailed: "false", wantOperational: "false", wantReaches: "false", wantSarif: true,
		},
		{
			name: "enabled with a parent root", root: "./..", against: "origin/main", sarif: "true",
			wantFailed: "false", wantOperational: "false", wantReaches: "false",
			wantWarning: "root must be '.'",
		},
		{
			name: "enabled with an absolute root", root: "/./", against: "origin/main", sarif: "true",
			wantFailed: "false", wantOperational: "false", wantReaches: "false",
			wantWarning: "root must be '.'",
		},
		{
			name: "enabled when the SARIF file is empty", root: ".", against: "origin/main", sarif: "true",
			sarifMode: "empty", sarifRC: "1",
			wantFailed: "false", wantOperational: "false", wantReaches: "false",
			wantWarning: "empty or invalid",
		},
		{
			name: "enabled when the SARIF file is not SARIF", root: ".", against: "origin/main", sarif: "true",
			sarifMode: "garbage", sarifRC: "1",
			wantFailed: "false", wantOperational: "false", wantReaches: "false",
			wantWarning: "empty or invalid",
		},
		{
			name: "enabled when the SARIF invocation exits 2", root: ".", against: "origin/main", sarif: "true",
			sarifRC:    "2",
			wantFailed: "false", wantOperational: "false", wantReaches: "false",
			wantWarning: "could not be rendered",
		},
		{
			name: "enabled with strict tests", root: ".", against: "origin/main", sarif: "true", strict: true,
			wantFailed: "false", wantOperational: "false", wantReaches: "false", wantSarif: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			stub := filepath.Join(dir, "weave-stub")
			if err := os.WriteFile(stub, []byte(stubWeave), 0o755); err != nil {
				t.Fatal(err)
			}
			script := filepath.Join(dir, "check.sh")
			if err := os.WriteFile(script, []byte(check.Run), 0o644); err != nil {
				t.Fatal(err)
			}
			outputs := filepath.Join(dir, "outputs")
			summary := filepath.Join(dir, "summary")
			calls := filepath.Join(dir, "calls.log")

			env := map[string]string{
				"WEAVE_BIN":           filepath.ToSlash(stub),
				"RUNNER_TEMP":         filepath.ToSlash(dir),
				"GITHUB_OUTPUT":       filepath.ToSlash(outputs),
				"GITHUB_STEP_SUMMARY": filepath.ToSlash(summary),
				"ROOT":                tc.root,
				"AGAINST":             tc.against,
				"STRICT_TESTS":        strconv.FormatBool(tc.strict),
				"SARIF":               tc.sarif,
				"FORK":                tc.fork,
				"STUB_RC":             strconv.Itoa(tc.rc),
				"STUB_REACHES":        strconv.FormatBool(tc.reaches),
				"STUB_SARIF":          tc.sarifMode,
				"STUB_SARIF_RC":       tc.sarifRC,
				"STUB_LOG":            filepath.ToSlash(calls),
			}
			cmd := exec.Command(shell, "./check.sh")
			cmd.Dir = dir
			cmd.Env = mergeEnv(env)
			var combined bytes.Buffer
			cmd.Stdout = &combined
			cmd.Stderr = &combined
			if err := cmd.Run(); err != nil {
				t.Fatalf("Check step must always exit 0, got %v:\n%s", err, combined.String())
			}

			got := readOutputs(t, outputs)
			if got["failed"] != tc.wantFailed {
				t.Errorf("failed = %q, want %q", got["failed"], tc.wantFailed)
			}
			if got["operational"] != tc.wantOperational {
				t.Errorf("operational = %q, want %q", got["operational"], tc.wantOperational)
			}
			if got["reaches"] != tc.wantReaches {
				t.Errorf("reaches = %q, want %q", got["reaches"], tc.wantReaches)
			}
			if got["rc"] != strconv.Itoa(tc.rc) {
				t.Errorf("rc = %q, want %d", got["rc"], tc.rc)
			}

			// The markdown gate is never sacrificed to SARIF.
			report := got["report"]
			if report == "" {
				t.Fatal("check step wrote no report output")
			}
			if !strings.HasPrefix(filepath.Clean(report), filepath.Clean(dir)) {
				t.Fatalf("report %q is outside RUNNER_TEMP", report)
			}
			body, err := os.ReadFile(report)
			if err != nil {
				t.Fatalf("markdown report: %v", err)
			}
			if !strings.Contains(string(body), "# weave check") {
				t.Fatalf("markdown report = %q", body)
			}
			if sum, err := os.ReadFile(summary); err != nil || string(sum) != string(body) {
				t.Fatalf("step summary = %q (err %v), want the report", sum, err)
			}

			log, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			callLog := string(log)
			if !strings.Contains(callLog, "check --root "+tc.root) {
				t.Errorf("check was not rooted at %q:\n%s", tc.root, callLog)
			}
			if tc.against != "" && !strings.Contains(callLog, "--against "+tc.against) {
				t.Errorf("check did not compare against %q:\n%s", tc.against, callLog)
			}
			if tc.strict && !strings.Contains(callLog, "--strict-tests") {
				t.Errorf("check did not pass --strict-tests:\n%s", callLog)
			}

			sarif := got["sarif"]
			sarifCall := strings.Contains(callLog, "--format sarif")
			wantCall := tc.wantSarif || tc.sarifMode != "" || tc.sarifRC != ""
			if sarifCall != wantCall {
				t.Fatalf("SARIF invocation = %v, want %v:\n%s", sarifCall, wantCall, callLog)
			}
			if tc.wantSarif {
				if sarif == "" {
					t.Fatalf("no sarif output; report was uploadable:\n%s", combined.String())
				}
				raw, err := os.ReadFile(sarif)
				if err != nil {
					t.Fatalf("SARIF file: %v", err)
				}
				if !strings.Contains(string(raw), `"2.1.0"`) || !strings.Contains(string(raw), `"runs"`) {
					t.Fatalf("SARIF file = %q", raw)
				}
			} else {
				if sarif != "" {
					t.Fatalf("sarif output = %q, want empty", sarif)
				}
				if _, err := os.Stat(filepath.Join(dir, "weave.sarif")); !os.IsNotExist(err) {
					t.Fatalf("weave.sarif exists without a valid upload (err %v)", err)
				}
			}
			if tc.wantWarning != "" && !strings.Contains(combined.String(), tc.wantWarning) {
				t.Errorf("missing warning %q:\n%s", tc.wantWarning, combined.String())
			}
		})
	}
}

const stubWeave = `#!/usr/bin/env bash
set -u
printf '%s\n' "$*" >> "$STUB_LOG"
fmt=text
prev=
for a in "$@"; do
  if [ "$prev" = "--format" ]; then fmt=$a; fi
  if [ "$a" = "--markdown" ]; then fmt=markdown; fi
  prev=$a
done
if [ "$fmt" = "markdown" ]; then
  printf '# weave check\n'
  if [ "${STUB_REACHES:-false}" = "true" ]; then
    printf '` + "```" + `mermaid\ngraph TD\n  A --> B\n` + "```" + `\n'
  fi
  exit "${STUB_RC:-0}"
fi
case "${STUB_SARIF:-valid}" in
  empty) ;;
  garbage) printf 'not sarif\n' ;;
  *) printf '{"version":"2.1.0","runs":[]}\n' ;;
esac
exit "${STUB_SARIF_RC:-${STUB_RC:-0}}"
`

func readOutputs(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("GITHUB_OUTPUT: %v", err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("malformed GITHUB_OUTPUT line %q", line)
		}
		out[key] = value
	}
	return out
}

// mergeEnv keeps the parent process usable on CI while overriding the variables
// the Check step reads. Last wins, and a variable present in the parent is
// replaced rather than duplicated.
func mergeEnv(overrides map[string]string) []string {
	drop := map[string]bool{}
	for key := range overrides {
		drop[key] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if ok && drop[key] {
			continue
		}
		env = append(env, kv)
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

// bashPath prefers Git Bash on Windows: C:\Windows\System32\bash.exe is the WSL
// launcher, which cannot run these scripts on a runner without a distribution.
func bashPath(t *testing.T) string {
	t.Helper()
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			`C:\Program Files\Git\bin\bash.exe`,
			`C:\Program Files\Git\usr\bin\bash.exe`,
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
		)
	}
	if found, err := exec.LookPath("bash"); err == nil {
		candidates = append(candidates, found)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err != nil && !filepath.IsAbs(candidate) {
			continue
		}
		cmd := exec.Command(candidate, "-c", "printf ok")
		if out, err := cmd.Output(); err == nil && strings.TrimSpace(string(out)) == "ok" {
			return candidate
		}
	}
	t.Skip("bash is required to execute the composite action's shell steps")
	return ""
}
