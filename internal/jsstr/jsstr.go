// Package jsstr reproduces the JavaScript string operations the TypeScript weave
// relies on, so the Go port counts, cuts and compares text the same way.
//
// JavaScript strings are UTF-16. A Go string here is UTF-8, except that a lone
// surrogate (what JS produces when a slice cuts an emoji in half) is kept as its
// three-byte WTF-8 form (ED A0..BF xx). jsjson writes those as \udXXX, exactly
// like JSON.stringify. Invalid UTF-8 bytes count as U+FFFD, which is what Node
// would have decoded them to.
package jsstr

import (
	"strings"
	"unicode/utf8"
)

// isSurrogate reports whether s[i:] starts with a WTF-8 encoded surrogate.
func isSurrogate(s string, i int) bool {
	return i+2 < len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF
}

// unitAt decodes the code point or lone surrogate at s[i], returning the value,
// its width in bytes and its length in UTF-16 code units.
func unitAt(s string, i int) (r rune, width, units int) {
	c := s[i]
	if c < utf8.RuneSelf {
		return rune(c), 1, 1
	}
	r, width = utf8.DecodeRuneInString(s[i:])
	if r == utf8.RuneError && width == 1 {
		if isSurrogate(s, i) {
			return 0xD000 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F), 3, 1
		}
		return utf8.RuneError, 1, 1
	}
	if r > 0xFFFF {
		return r, width, 2
	}
	return r, width, 1
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// Len is String.prototype.length: the number of UTF-16 code units.
func Len(s string) int {
	n := 0
	for i := 0; i < len(s); {
		_, w, u := unitAt(s, i)
		i += w
		n += u
	}
	return n
}

// Units returns the UTF-16 code units of s.
func Units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, w, u := unitAt(s, i)
		i += w
		if u == 2 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

// FromUnits builds a string from UTF-16 code units. Valid pairs become UTF-8,
// lone surrogates stay WTF-8 so they survive a round trip.
func FromUnits(u []uint16) string {
	var b strings.Builder
	b.Grow(len(u))
	for i := 0; i < len(u); i++ {
		c := rune(u[i])
		if c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF {
			b.WriteRune(0x10000 + (c-0xD800)<<10 + rune(u[i+1]) - 0xDC00)
			i++
			continue
		}
		if c >= 0xD800 && c <= 0xDFFF {
			b.WriteByte(byte(0xE0 | c>>12))
			b.WriteByte(byte(0x80 | (c>>6)&0x3F))
			b.WriteByte(byte(0x80 | c&0x3F))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// Slice is String.prototype.slice(start, end): UTF-16 indices, negative values
// count from the end, and a cut through a surrogate pair leaves a lone half.
func Slice(s string, start, end int) string {
	if isASCII(s) {
		start, end = bounds(len(s), start, end)
		return s[start:end]
	}
	u := Units(s)
	start, end = bounds(len(u), start, end)
	return FromUnits(u[start:end])
}

// SliceFrom is String.prototype.slice(start) with no end.
func SliceFrom(s string, start int) string {
	return Slice(s, start, Len(s))
}

func bounds(n, start, end int) (int, int) {
	clamp := func(i int) int {
		if i < 0 {
			i += n
			if i < 0 {
				return 0
			}
		}
		if i > n {
			return n
		}
		return i
	}
	start, end = clamp(start), clamp(end)
	if start > end {
		end = start
	}
	return start, end
}

// CompareUTF16 is JavaScript's `<` on strings and the order of a default
// Array.prototype.sort: by UTF-16 code unit. It equals byte order except that
// U+E000..U+FFFF sorts after astral characters (whose high surrogates are lower).
func CompareUTF16(a, b string) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	if i == len(a) || i == len(b) {
		return cmpInt(len(a), len(b))
	}
	for i > 0 && (a[i]&0xC0 == 0x80 || b[i]&0xC0 == 0x80) { // back up to the start of the differing character
		i--
	}
	ua, ub := Units(a[i:]), Units(b[i:])
	for k := 0; k < len(ua) && k < len(ub); k++ {
		if ua[k] != ub[k] {
			return cmpInt(int(ua[k]), int(ub[k]))
		}
	}
	return cmpInt(len(ua), len(ub))
}

func cmpInt(x, y int) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// IsSpace is the JS WhiteSpace and LineTerminator set that String.prototype.trim
// removes. Unlike unicode.IsSpace it includes U+FEFF (BOM) and excludes U+0085.
func IsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// Trim is String.prototype.trim.
func Trim(s string) string { return strings.TrimFunc(s, IsSpace) }

// TrimStart is String.prototype.trimStart.
func TrimStart(s string) string { return strings.TrimLeftFunc(s, IsSpace) }

// TrimEnd is String.prototype.trimEnd.
func TrimEnd(s string) string { return strings.TrimRightFunc(s, IsSpace) }

// PadEnd is String.prototype.padEnd(n) with the default space filler.
func PadEnd(s string, n int) string { return PadEndWith(s, n, " ") }

// PadStart is String.prototype.padStart(n) with the default space filler.
func PadStart(s string, n int) string { return PadStartWith(s, n, " ") }

// PadEndWith is String.prototype.padEnd(n, fill).
func PadEndWith(s string, n int, fill string) string { return s + filler(s, n, fill) }

// PadStartWith is String.prototype.padStart(n, fill).
func PadStartWith(s string, n int, fill string) string { return filler(s, n, fill) + s }

func filler(s string, n int, fill string) string {
	need := n - Len(s)
	if need <= 0 || fill == "" {
		return ""
	}
	if isASCII(fill) {
		return strings.Repeat(fill, need/len(fill)+1)[:need]
	}
	u := Units(fill)
	out := make([]uint16, 0, need)
	for len(out) < need {
		out = append(out, u[:min(len(u), need-len(out))]...)
	}
	return FromUnits(out)
}

// DecodeUTF8 is Buffer.toString('utf8') / readFileSync(p, 'utf8'): the WHATWG
// decoder, which replaces each maximal invalid subpart with one U+FFFD. A BOM is
// kept, as Node keeps it.
func DecodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	out.Grow(len(b) + 8)
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			out.WriteByte(c)
			i++
			continue
		}
		need, lo, hi := 0, byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c == 0xE0:
			need, lo = 2, 0xA0
		case c == 0xED:
			need, hi = 2, 0x9F
		case c >= 0xE1 && c <= 0xEF:
			need = 2
		case c == 0xF0:
			need, lo = 3, 0x90
		case c == 0xF4:
			need, hi = 3, 0x8F
		case c >= 0xF1 && c <= 0xF3:
			need = 3
		default:
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j := 1
		for ; j <= need; j++ {
			if i+j >= len(b) || b[i+j] < lo || b[i+j] > hi {
				break
			}
			lo, hi = 0x80, 0xBF
		}
		if j <= need {
			out.WriteRune(utf8.RuneError)
			i += j
			continue
		}
		out.Write(b[i : i+need+1])
		i += need + 1
	}
	return out.String()
}

// SplitLines is text.split(/\r?\n/).
func SplitLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines)-1; i++ {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}
