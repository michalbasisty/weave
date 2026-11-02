package main

import (
	"fmt"
	"os"
)

// Directory order as Go sees it: sorted by filename on every OS.
func main() {
	entries, err := os.ReadDir(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, e := range entries {
		fmt.Println(e.Name())
	}
}
