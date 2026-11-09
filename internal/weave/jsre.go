package weave

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/michalbasisty/weave/internal/jsstr"
)

// jsSpace is the body of JavaScript's \s class: ASCII whitespace plus the Unicode
// spaces, line separators and the BOM. RE2's \s is ASCII only.
const jsSpace = `\t\n\v\f\r \x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

// jsDot is JavaScript's `.` without the s flag: anything but a line terminator.
const jsDot = `[^\n\r\x{2028}\x{2029}]`

// jsSource rewrites a JavaScript regex source so RE2 matches the same text:
// \s and \S use the JS whitespace set and `.` stops at every line terminator.
// \w, \d and \b are ASCII in both. Lookarounds and backreferences are not
// translated; the extractors never use them.
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

// jsRe compiles a JavaScript regex source for RE2. Use it only for patterns that
// start with a literal or run on short strings: a leading \b or class defeats
// RE2's prefix scan (measured 100x slower than V8 on a 9 MB corpus), so the hot ones are scanners.
func jsRe(src string) *regexp.Regexp { return regexp.MustCompile(jsSource(src)) }

// ---- character helpers shared by the scanners ----

// isWord is \w: the characters that decide \b.
func isWord(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isUpperDigit(c byte) bool { return (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') }

// boundaryBefore is \b in front of a word character at s[i].
func boundaryBefore(s string, i int) bool { return i == 0 || !isWord(s[i-1]) }

// spaceAt is the byte width of the JS whitespace character at s[i], or 0.
func spaceAt(s string, i int) int {
	c := s[i]
	if c < utf8.RuneSelf {
		if c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r' {
			return 1
		}
		return 0
	}
	if c != 0xC2 && c != 0xE1 && c != 0xE2 && c != 0xE3 && c != 0xEF {
		return 0
	}
	r, w := utf8.DecodeRuneInString(s[i:])
	if jsstr.IsSpace(r) {
		return w
	}
	return 0
}

// skipSpace is \s* forward from i.
func skipSpace(s string, i int) int {
	for i < len(s) {
		w := spaceAt(s, i)
		if w == 0 {
			break
		}
		i += w
	}
	return i
}

// skipSpaceBack is \s* backward from i: the start of the whitespace ending at i.
func skipSpaceBack(s string, i int) int {
	for i > 0 {
		c := s[i-1]
		if c < utf8.RuneSelf {
			if c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r' {
				i--
				continue
			}
			return i
		}
		r, w := utf8.DecodeLastRuneInString(s[:i])
		if !jsstr.IsSpace(r) {
			return i
		}
		i -= w
	}
	return i
}

// collapseSpace is s.replace(/\s+/g, ' ').
func collapseSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if w := spaceAt(s, i); w > 0 {
			for i < len(s) {
				w = spaceAt(s, i)
				if w == 0 {
					break
				}
				i += w
			}
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isQuote3(c byte) bool { return c == '\'' || c == '"' || c == '`' }
func isQuote2(c byte) bool { return c == '\'' || c == '"' }

// lineAt is the 1-based line of a byte offset: text.slice(0, i).split('\n').length.
func lineAt(text string, index int) int {
	if index > len(text) {
		index = len(text)
	}
	return strings.Count(text[:index], "\n") + 1
}

// isCode is /\.(t|j)sx?$/.
func isCode(rel string) bool {
	return strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx") ||
		strings.HasSuffix(rel, ".js") || strings.HasSuffix(rel, ".jsx")
}
