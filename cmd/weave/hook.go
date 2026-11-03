package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A fire is one run of the gate by a hook. It is the only honest measure of whether
// the in-session check earns its keep: history replay cannot see a break that is
// introduced and fixed between two commits.
type fire struct {
	At     string   `json:"at"`
	Result string   `json:"result"`
	Ms     int64    `json:"ms"`
	Fails  []string `json:"fails,omitempty"`
	Ref    string   `json:"ref,omitempty"`
}

func firesPath() string { return filepath.Join(root, ".weave", "fires.jsonl") }

func appendFire(f fire) {
	p := firesPath()
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	fh, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer fh.Close()
	_, _ = fh.Write(append(b, '\n'))
}

func readFires() []fire {
	fh, err := os.Open(firesPath())
	if err != nil {
		return nil
	}
	defer fh.Close()
	var out []fire
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var f fire
		if json.Unmarshal(sc.Bytes(), &f) == nil {
			out = append(out, f)
		}
	}
	return out
}

// failingLines keeps the one line per failure that a hook or a human can act on.
func failingLines(out string) []string {
	var fails []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FAIL ") {
			fails = append(fails, strings.TrimPrefix(line, "FAIL "))
		}
	}
	return fails
}

// cmdHook is the entry point for an agent hook: it runs the gate, records the fire,
// and on a broken contract writes the failure to stderr and exits 2, which is what a
// Claude Code Stop hook needs to keep the turn going. Success is silent.
func cmdHook() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	args := []string{"check", "--root", root}
	if a := arg("against", ""); a != "" {
		args = append(args, "--against", a)
	}
	if hasFlag("strict-tests") {
		args = append(args, "--strict-tests")
	}
	start := time.Now()
	cmd := exec.Command(exe, args...)
	out, runErr := cmd.CombinedOutput()
	code := 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			fmt.Fprintln(os.Stderr, "weave hook:", runErr)
			return 1
		}
	}
	text := string(out)
	rec := fire{
		At:     time.Now().UTC().Format(time.RFC3339),
		Result: "pass",
		Ms:     time.Since(start).Milliseconds(),
		Ref:    arg("against", ""),
	}
	if code != 0 {
		rec.Result = "fail"
		rec.Fails = failingLines(text)
	}
	appendFire(rec)

	if code != 0 {
		fmt.Fprint(os.Stderr, strings.TrimSpace(text))
		fmt.Fprint(os.Stderr, "\n\nweave: this contract is broken. Fix the call site or the endpoint, or say why it is intentional, before finishing.\n")
		return 2
	}
	return 0
}

// cmdFires reports what the hook has seen: runs, failures, and the failures
// themselves. A hook that never fires, or one that only ever fails on false alarms,
// is the signal to stop investing in it.
func cmdFires() int {
	since := intArg("since", "0")
	list := readFires()
	if since > 0 {
		cut := time.Now().Add(-time.Duration(since) * 24 * time.Hour)
		kept := list[:0]
		for _, f := range list {
			if t, err := time.Parse(time.RFC3339, f.At); err == nil && t.After(cut) {
				kept = append(kept, f)
			}
		}
		list = kept
	}
	if len(list) == 0 {
		fmt.Println("fires: none recorded")
		fmt.Println("  install the hook once: weave init --hooks")
		return 0
	}
	var fails []fire
	var total int64
	for _, f := range list {
		total += f.Ms
		if f.Result == "fail" {
			fails = append(fails, f)
		}
	}
	window := "all time"
	if since > 0 {
		window = strconv.Itoa(since) + " day(s)"
	}
	fmt.Printf("fires: %d run(s) over %s · %d failure(s) · avg %dms\n", len(list), window, len(fails), total/int64(len(list)))
	last := list[len(list)-1]
	when, _ := time.Parse(time.RFC3339, last.At)
	fmt.Printf("  last    %s  %s  %dms\n", when.Format("2006-01-02 15:04"), last.Result, last.Ms)
	for i := len(fails) - 1; i >= 0 && i >= len(fails)-8; i-- {
		f := fails[i]
		t, _ := time.Parse(time.RFC3339, f.At)
		what := strings.Join(f.Fails, "; ")
		if what == "" {
			what = "(no line captured)"
		}
		fmt.Printf("  fail    %s  %s\n", t.Format("2006-01-02 15:04"), what)
	}
	return 0
}
