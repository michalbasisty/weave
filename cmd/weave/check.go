package main

import (
	"fmt"
	"os"

	"github.com/michalbasisty/weave/internal/verify"
	"github.com/michalbasisty/weave/internal/weave"
)

// checkFormats is every report `weave check` can render. The key is what
// --format accepts; an empty value is the default text report.
var checkFormats = map[string]bool{
	"": true, "text": true, "markdown": true, "json": true, "sarif": true,
}

// checkFormat is the requested report. --markdown is the old spelling; the
// command rejects it together with --format before this runs.
func checkFormat() string {
	if hasFlag("markdown") {
		return "markdown"
	}
	if f := arg("format", ""); f != "" {
		return f
	}
	return "text"
}

// validCheckFormat rejects an unknown format before the gate reads or writes
// anything, so a typo cannot reindex a tree or leave a stray report behind.
func validCheckFormat(format string) bool { return checkFormats[format] }

// checkFormatProblem explains why the requested report cannot be rendered, or
// "" when it can. One report has one spelling: --markdown and --format together
// are refused instead of silently letting one win.
func checkFormatProblem() string {
	format := arg("format", "")
	if hasFlag("markdown") {
		if format != "" {
			return "--markdown and --format cannot be combined; use one"
		}
		return ""
	}
	if !validCheckFormat(format) {
		return fmt.Sprintf("unknown format %q for check (want text, markdown, json or sarif)", format)
	}
	return ""
}

func cmdCheck() int {
	format := checkFormat()
	g, err := weave.Reindex(root)
	if err != nil {
		// The graph could not be built: operational, not a contract failure. In
		// markdown mode the report must still carry the marker and a diagnosis,
		// because the Action posts and summarizes the file verbatim. JSON and
		// SARIF get the same run as data, never a verdict.
		res := verify.OperationalResult("the graph could not be built", err.Error())
		return emitCheck(res, format, err)
	}
	res := verify.Run(g, root, arg("against", ""), hasFlag("strict-tests"), pos)
	return emitCheck(res, format, nil)
}

// emitCheck renders one result in the requested format. A render that fails
// keeps the run operational: a broken report is never mistaken for a clean one.
func emitCheck(res verify.Result, format string, cause error) int {
	switch format {
	case "markdown":
		fmt.Print(res.Markdown())
	case "json", "sarif":
		var out []byte
		var err error
		if format == "json" {
			out, err = res.JSON()
		} else {
			out, err = res.SARIF()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println(string(out))
	default:
		if cause != nil {
			fmt.Fprintln(os.Stderr, cause)
			return 2
		}
		fmt.Println(res.Text())
	}
	return res.Code()
}
