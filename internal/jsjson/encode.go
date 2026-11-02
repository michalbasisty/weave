package jsjson

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Marshal is JSON.stringify(v): compact, no trailing newline.
func Marshal(v Value) []byte { return Append(nil, v) }

// Append appends JSON.stringify(v) to dst.
func Append(dst []byte, v Value) []byte {
	switch v.kind {
	case KindBool:
		if v.b {
			return append(dst, "true"...)
		}
		return append(dst, "false"...)
	case KindNumber:
		return append(dst, v.s...)
	case KindString:
		return AppendString(dst, v.s)
	case KindArray:
		dst = append(dst, '[')
		for i, x := range v.a {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = Append(dst, x)
		}
		return append(dst, ']')
	case KindObject:
		dst = append(dst, '{')
		for i := 0; i < v.o.Len(); i++ {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = AppendString(dst, v.o.keys[i])
			dst = append(dst, ':')
			dst = Append(dst, v.o.vals[i])
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

const hexDigits = "0123456789abcdef"

// AppendString appends s as JSON.stringify writes a string: short escapes for
// \b \t \n \f \r " \, \u00xx for other C0 controls, everything else raw
// (no HTML escaping, U+2028/U+2029 raw). A WTF-8 lone surrogate is written as
// \udxxx; two WTF-8 halves that form a pair are written as the character, as a
// JS string holding them would be. Invalid bytes are written as U+FFFD.
func AppendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' && c < utf8.RuneSelf {
			i++
			continue
		}
		if c < utf8.RuneSelf {
			dst = append(dst, s[start:i]...)
			switch c {
			case '"', '\\':
				dst = append(dst, '\\', c)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\t':
				dst = append(dst, '\\', 't')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\r':
				dst = append(dst, '\\', 'r')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			}
			i++
			start = i
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r != utf8.RuneError || w != 1 {
			i += w
			continue
		}
		dst = append(dst, s[start:i]...)
		if u, ok := surrogateAt(s, i); ok {
			if lo, ok := surrogateAt(s, i+3); ok && u <= 0xDBFF && lo >= 0xDC00 {
				dst = utf8.AppendRune(dst, 0x10000+(u-0xD800)<<10+lo-0xDC00)
				i += 6
			} else {
				dst = append(dst, '\\', 'u', 'd', hexDigits[u>>8&0xF], hexDigits[u>>4&0xF], hexDigits[u&0xF])
				i += 3
			}
		} else {
			dst = utf8.AppendRune(dst, utf8.RuneError)
			i++
		}
		start = i
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

// surrogateAt decodes a WTF-8 surrogate at s[i].
func surrogateAt(s string, i int) (rune, bool) {
	if i+2 < len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
		return 0xD000 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F), true
	}
	return 0, false
}

// FormatNumber is Number.prototype.toString(): the shortest round-tripping
// digits, positional between 1e-7 and 1e21, exponential outside.
func FormatNumber(f float64) string {
	switch {
	case f != f:
		return "NaN"
	case f == 0:
		return "0"
	case f > 1.7976931348623157e308:
		return "Infinity"
	case f < -1.7976931348623157e308:
		return "-Infinity"
	case f < 0:
		return "-" + FormatNumber(-f)
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // d.ddde±XX
	mant, exp, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	x, _ := strconv.Atoi(exp)
	k, n := len(digits), x+1
	switch {
	case k <= n && n <= 21:
		return digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return "0." + strings.Repeat("0", -n) + digits
	}
	out := digits[:1]
	if k > 1 {
		out += "." + digits[1:]
	}
	if n-1 >= 0 {
		return out + "e+" + strconv.Itoa(n-1)
	}
	return out + "e-" + strconv.Itoa(1-n)
}
