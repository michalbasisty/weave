package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseArgsRejectsUnknownOption(t *testing.T) {
	if _, _, err := parseArgs([]string{"check", "--bogus"}); err == nil || !strings.Contains(err.Error(), "unknown option --bogus") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseArgsRejectsMissingValue(t *testing.T) {
	cases := [][]string{
		{"check", "--against"},
		{"check", "--against", "--strict-tests"},
		{"check", "--root", "--"},
	}
	for _, argv := range cases {
		if _, _, err := parseArgs(argv); err == nil || !strings.Contains(err.Error(), "needs a value") {
			t.Errorf("parseArgs(%q) err = %v", argv, err)
		}
	}
}

func TestParseArgsPreservesOptionsAndDoubleDash(t *testing.T) {
	cmd, pos, err := parseArgs([]string{
		"check", "--root", ".", "--strict-tests", "--markdown", "a.ts", "--", "--bogus", "b.ts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "check" {
		t.Fatalf("cmd = %q", cmd)
	}
	if got := strings.Join(pos, ","); got != "a.ts,--bogus,b.ts" {
		t.Fatalf("pos = %q", got)
	}
}

func TestParseArgsHelpAndVersion(t *testing.T) {
	if cmd, _, err := parseArgs([]string{"--help"}); err != nil || cmd != "help" {
		t.Fatalf("help: %q %v", cmd, err)
	}
	if cmd, _, err := parseArgs([]string{"-v"}); err != nil || cmd != "version" {
		t.Fatalf("version: %q %v", cmd, err)
	}
}

func TestKnownCommand(t *testing.T) {
	for _, c := range []string{"check", "hook", "spec", "help", "version"} {
		if !knownCommand(c) {
			t.Errorf("%q should be known", c)
		}
	}
	for _, c := range []string{"bogus", "chek", ""} {
		if knownCommand(c) {
			t.Errorf("%q should be unknown", c)
		}
	}
}

// -- ends option parsing for the accessors too, not just for parseArgs.
func TestOptionsStopAtDoubleDash(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()

	os.Args = []string{"weave", "check", "--root", "here", "--file", "a.ts", "--", "--root", "there", "--markdown"}
	if got := arg("root", ""); got != "here" {
		t.Errorf("arg(root) = %q, want here", got)
	}
	if hasFlag("markdown") {
		t.Error("hasFlag read an option after --")
	}
	if got := strings.Join(flagValues("file"), ","); got != "a.ts" {
		t.Errorf("flagValues(file) = %q", got)
	}
}
