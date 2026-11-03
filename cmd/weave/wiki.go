package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/michalbasisty/weave/internal/weave"
)

// gitHead is the commit the wiki is stamped with, or "" outside git.
func gitHead(dir string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func cmdWiki() int {
	out := arg("out", "")
	if out == "" {
		out = filepath.Join(root, ".weave", "wiki")
	} else if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	head := gitHead(root)

	// --check answers the only question a generated wiki has to answer: is it still
	// describing this commit?
	if hasFlag("check") {
		have := weave.ReadWikiStamp(out)
		switch {
		case have == "":
			fmt.Printf("wiki: none at %s — run `weave wiki`\n", out)
			return 1
		case head != "" && have != head:
			fmt.Printf("wiki: stale — pages describe %s, HEAD is %s\n  run `weave wiki`\n", have[:min(7, len(have))], head[:min(7, len(head))])
			return 1
		}
		fmt.Printf("wiki: current (%s)\n", have[:min(7, len(have))])
		return 0
	}

	g, code := graphFor()
	if g == nil {
		return code
	}
	res, err := weave.WriteWiki(g, weave.WikiOptions{OutDir: out, Commit: head})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("wiki: %d page(s) in %s\n", res.Pages, out)
	fmt.Printf("  open %s\n", res.Index)
	if head == "" {
		fmt.Println("  no git history: pages are not stamped, so `weave wiki --check` cannot tell you they are current")
	}
	return 0
}
