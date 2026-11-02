package jsjson

import (
	"fmt"
	"unicode/utf8"
)

// SyntaxError is a JSON.parse failure. Its message is not V8's.
type SyntaxError struct {
	Offset int
	Msg    string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("jsjson: %s at offset %d", e.Msg, e.Offset)
}

// Parse is JSON.parse on already decoded text. A leading BOM is an error, as it
// is for JSON.parse.
func Parse(data []byte) (Value, error) {
	p := parser{d: data}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.space()
	if p.i < len(p.d) {
		return Value{}, p.fail("unexpected data after JSON")
	}
	return v, nil
}

const maxDepth = 10000

type parser struct {
	d []byte
	i int
}

func (p *parser) fail(msg string) error { return &SyntaxError{Offset: p.i, Msg: msg} }

func (p *parser) space() {
	for p.i < len(p.d) {
		switch p.d[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) literal(word string) bool {
	if len(p.d)-p.i >= len(word) && string(p.d[p.i:p.i+len(word)]) == word {
		p.i += len(word)
		return true
	}
	return false
}

func (p *parser) value(depth int) (Value, error) {
	if depth > maxDepth {
		return Value{}, p.fail("nesting too deep")
	}
	if p.i >= len(p.d) {
		return Value{}, p.fail("unexpected end of JSON input")
	}
	switch c := p.d[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, err := p.str()
		return Str(s), err
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case p.literal("true"):
		return Bool(true), nil
	case p.literal("false"):
		return Bool(false), nil
	case p.literal("null"):
		return Null(), nil
	}
	return Value{}, p.fail(fmt.Sprintf("unexpected token %q", p.d[p.i]))
}

func (p *parser) object(depth int) (Value, error) {
	p.i++
	o := &Object{}
	p.space()
	if p.i < len(p.d) && p.d[p.i] == '}' {
		p.i++
		return Obj(o), nil
	}
	for {
		p.space()
		if p.i >= len(p.d) || p.d[p.i] != '"' {
			return Value{}, p.fail("expected property name")
		}
		k, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.space()
		if p.i >= len(p.d) || p.d[p.i] != ':' {
			return Value{}, p.fail("expected ':'")
		}
		p.i++
		p.space()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		o.Set(k, v)
		p.space()
		if p.i >= len(p.d) {
			return Value{}, p.fail("unterminated object")
		}
		switch p.d[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return Obj(o), nil
		default:
			return Value{}, p.fail("expected ',' or '}'")
		}
	}
}

func (p *parser) array(depth int) (Value, error) {
	p.i++
	vs := []Value{}
	p.space()
	if p.i < len(p.d) && p.d[p.i] == ']' {
		p.i++
		return Arr(vs...), nil
	}
	for {
		p.space()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		vs = append(vs, v)
		p.space()
		if p.i >= len(p.d) {
			return Value{}, p.fail("unterminated array")
		}
		switch p.d[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return Arr(vs...), nil
		default:
			return Value{}, p.fail("expected ',' or ']'")
		}
	}
}

func (p *parser) number() (Value, error) {
	start := p.i
	digits := func() int {
		n := 0
		for p.i < len(p.d) && p.d[p.i] >= '0' && p.d[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.d[p.i] == '-' {
		p.i++
	}
	if p.i < len(p.d) && p.d[p.i] == '0' {
		p.i++
	} else if digits() == 0 {
		return Value{}, p.fail("invalid number")
	}
	if p.i < len(p.d) && p.d[p.i] == '.' {
		p.i++
		if digits() == 0 {
			return Value{}, p.fail("invalid number")
		}
	}
	if p.i < len(p.d) && (p.d[p.i] == 'e' || p.d[p.i] == 'E') {
		p.i++
		if p.i < len(p.d) && (p.d[p.i] == '+' || p.d[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			return Value{}, p.fail("invalid number")
		}
	}
	return Value{kind: KindNumber, s: string(p.d[start:p.i])}, nil
}

// str reads a string literal. A \u escape for a lone surrogate is kept as
// WTF-8, so it can be written back as the same escape.
func (p *parser) str() (string, error) {
	p.i++
	start := p.i
	for p.i < len(p.d) {
		c := p.d[p.i]
		if c == '"' {
			s := string(p.d[start:p.i])
			p.i++
			return s, nil
		}
		if c == '\\' {
			break
		}
		if c < 0x20 {
			return "", p.fail("bad control character in string literal")
		}
		p.i++
	}
	buf := append([]byte(nil), p.d[start:p.i]...)
	for p.i < len(p.d) {
		c := p.d[p.i]
		switch {
		case c == '"':
			p.i++
			return string(buf), nil
		case c < 0x20:
			return "", p.fail("bad control character in string literal")
		case c != '\\':
			buf = append(buf, c)
			p.i++
			continue
		}
		p.i++
		if p.i >= len(p.d) {
			break
		}
		e := p.d[p.i]
		p.i++
		switch e {
		case '"', '\\', '/':
			buf = append(buf, e)
		case 'b':
			buf = append(buf, '\b')
		case 'f':
			buf = append(buf, '\f')
		case 'n':
			buf = append(buf, '\n')
		case 'r':
			buf = append(buf, '\r')
		case 't':
			buf = append(buf, '\t')
		case 'u':
			u, ok := p.hex4()
			if !ok {
				return "", p.fail("bad unicode escape")
			}
			if u >= 0xD800 && u <= 0xDBFF && p.i+1 < len(p.d) && p.d[p.i] == '\\' && p.d[p.i+1] == 'u' {
				save := p.i
				p.i += 2
				if lo, ok := p.hex4(); ok && lo >= 0xDC00 && lo <= 0xDFFF {
					buf = utf8.AppendRune(buf, 0x10000+(u-0xD800)<<10+lo-0xDC00)
					continue
				}
				p.i = save
			}
			buf = appendUnit(buf, u)
		default:
			p.i--
			return "", p.fail("bad escaped character")
		}
	}
	return "", p.fail("unterminated string")
}

func (p *parser) hex4() (rune, bool) {
	if len(p.d)-p.i < 4 {
		return 0, false
	}
	var r rune
	for _, c := range p.d[p.i : p.i+4] {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			r |= rune(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	p.i += 4
	return r, true
}

// appendUnit writes a BMP code unit; surrogates become WTF-8.
func appendUnit(buf []byte, u rune) []byte {
	if u >= 0xD800 && u <= 0xDFFF {
		return append(buf, byte(0xE0|u>>12), byte(0x80|(u>>6)&0x3F), byte(0x80|u&0x3F))
	}
	return utf8.AppendRune(buf, u)
}
