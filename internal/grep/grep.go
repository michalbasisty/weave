// Package grep is the Go port of src/grep.ts: literal or regex text search over
// the files weave already indexes, graph files ranked first.
package grep

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/weave"
)

// maxLine is the longest line a hit may be on: anything longer is minified or generated.
const maxLine = 400

// maxHits bounds a scan. The TypeScript checks after pushing and breaks the line
// loop only, so each later file can still add one hit; that is reproduced.
const maxHits = 5000

type hit struct {
	file string
	line int
	text string
	rank int
}

// Grep returns the matching lines, ranked graph-first, then by file and line.
// It uses the per-root text cache, so repeated searches in one process only
// re-read files that changed.
func Grep(g *weave.Graph, query string, limit int, useRegex bool) string {
	return Cached(g, query, limit, useRegex)
}

func render(g *weave.Graph, query string, limit int, useRegex bool, files []cachedLines) string {
	q := jsstr.Trim(query)
	if q == "" {
		return "grep needs a query"
	}

	inGraph := map[string]bool{}
	for _, n := range g.Nodes {
		if n.File != nil && *n.File != "" {
			inGraph[weave.ToPosix(*n.File)] = true
		}
	}

	var hits []hit
	if useRegex {
		re, err := compile(q, useRegex)
		if err != nil {
			return "bad regex: " + badRegex(q, err)
		}
		for _, f := range files {
			for i, line := range f.lines {
				if jsstr.Len(line) > maxLine {
					continue
				}
				if !re.MatchString(line) {
					continue
				}
				text := jsstr.Slice(jsstr.Trim(line), 0, 160)
				rank := 1
				if inGraph[f.rel] {
					rank = 0
				}
				hits = append(hits, hit{f.rel, i + 1, text, rank})
				if len(hits) > maxHits {
					break
				}
			}
		}
	} else {
		needle := strings.ToLower(q)
		for _, f := range files {
			rank := 1
			if inGraph[f.rel] {
				rank = 0
			}
			from := 0
			for {
				j := strings.Index(f.lower[from:], needle)
				if j < 0 {
					break
				}
				pos := from + j
				lineIdx := sort.SearchInts(f.starts, pos+1) - 1
				lineEnd := len(f.lower)
				if lineIdx >= 0 && lineIdx < len(f.lines) {
					line := f.lines[lineIdx]
					if jsstr.Len(line) <= maxLine {
						hits = append(hits, hit{f.rel, lineIdx + 1, jsstr.Slice(jsstr.Trim(line), 0, 160), rank})
						if len(hits) > maxHits {
							break
						}
					}
				}
				if lineIdx+1 < len(f.starts) {
					lineEnd = f.starts[lineIdx+1]
				}
				if lineEnd <= from {
					lineEnd = from + len(needle)
				}
				from = lineEnd
			}
		}
	}
	if len(hits) == 0 {
		return `grep: no match for "` + q + `"`
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		if c := jsstr.LocaleCompare(hits[i].file, hits[j].file); c != 0 {
			return c < 0
		}
		return hits[i].line < hits[j].line
	})

	cap := limit
	if cap < 1 {
		cap = 1
	}
	shown := hits
	if len(shown) > cap {
		shown = shown[:cap]
	}
	header := fmt.Sprintf("grep: %d", len(hits))
	if len(hits) > len(shown) {
		header += fmt.Sprintf(" (+%d more)", len(hits)-len(shown))
	}
	header += ` line(s) for "` + q + `"`

	out := make([]string, 0, len(shown)+1)
	out = append(out, header)
	for _, h := range shown {
		out = append(out, fmt.Sprintf("  %s:%d: %s", h.file, h.line, h.text))
	}
	return strings.Join(out, "\n")
}

// compile builds the case-insensitive pattern the TypeScript passes to RegExp.
func compile(q string, useRegex bool) (*regexp.Regexp, error) {
	src := regexp.QuoteMeta(q)
	if useRegex {
		src = jsSource(q)
	}
	return regexp.Compile("(?i)" + src)
}

// badRegex turns the common RE2 failures into the message V8 prints, so a caller
// comparing output sees the same line.
func badRegex(q string, err error) string {
	msg := err.Error()
	reason := ""
	switch {
	case strings.Contains(msg, "missing closing ]"):
		reason = "Unterminated character class"
	case strings.Contains(msg, "missing closing )"):
		reason = "Unterminated group"
	case strings.Contains(msg, "trailing backslash"):
		reason = `\ at end of pattern`
	case strings.Contains(msg, "missing argument to repetition operator"):
		reason = "Nothing to repeat"
	}
	if reason == "" {
		return msg
	}
	return fmt.Sprintf("Invalid regular expression: /%s/i: %s", q, reason)
}

// jsSpace is the body of JavaScript's \s class; RE2's \s is ASCII only.
const jsSpace = `\t\n\v\f\r \x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

// jsDot is JavaScript's `.` without the s flag: anything but a line terminator.
const jsDot = `[^\n\r\x{2028}\x{2029}]`

// jsSource rewrites a JavaScript regex source so RE2 matches the same text.
// Lookarounds and backreferences are not translated; the TypeScript extractors
// never use them, and a caller who does gets RE2's error.
func jsSource(src string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\\' && i+1 < len(src):
			i++
			d := src[i]
			switch {
			case d == 's' && inClass:
				b.WriteString(jsSpace)
			case d == 's':
				b.WriteString("[" + jsSpace + "]")
			case d == 'S' && !inClass:
				b.WriteString("[^" + jsSpace + "]")
			case d == 'u' && i+1 < len(src) && src[i+1] == '{':
				if end := strings.IndexByte(src[i+2:], '}'); end >= 0 {
					b.WriteString(`\x{` + src[i+2:i+2+end] + `}`)
					i += 2 + end
				} else {
					b.WriteString(`\u`)
				}
			case d == 'u' && i+4 < len(src) && isHex4(src[i+1:i+5]):
				b.WriteString(`\x{` + src[i+1:i+5] + `}`)
				i += 4
			default:
				b.WriteByte('\\')
				b.WriteByte(d)
			}
		case c == '[' && !inClass:
			inClass = true
			b.WriteByte(c)
		case c == ']' && inClass:
			inClass = false
			b.WriteByte(c)
		case c == '.' && !inClass:
			b.WriteString(jsDot)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isHex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
