package jsstr

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLenAndSlice(t *testing.T) {
	cases := []struct {
		s string
		n int
	}{
		{"", 0}, {"abc", 3}, {"zażółć", 6}, {"😀", 2}, {"a😀b", 4}, {"\xff", 1}, {"€", 1},
	}
	for _, c := range cases {
		if got := Len(c.s); got != c.n {
			t.Errorf("Len(%q) = %d, want %d", c.s, got, c.n)
		}
	}

	high := Slice("😀", 0, 1)
	if high != "\xed\xa0\xbd" || Len(high) != 1 {
		t.Fatalf("Slice(😀, 0, 1) = %x, want the lone high surrogate ed a0 bd", high)
	}
	if u := Units(high); len(u) != 1 || u[0] != 0xD83D {
		t.Fatalf("Units(high) = %x", u)
	}
	if low := SliceFrom("😀", 1); Units(low)[0] != 0xDE00 {
		t.Fatalf("SliceFrom(😀, 1) = %x", Units(low))
	}
	if joined := FromUnits(append(Units(high), 0xDE00)); joined != "😀" {
		t.Fatalf("rejoined halves = %q", joined)
	}

	slices := []struct {
		s          string
		start, end int
		want       string
	}{
		{"abcdef", 1, 3, "bc"}, {"abcdef", -2, 6, "ef"}, {"abcdef", 4, 2, ""}, {"abcdef", 0, 99, "abcdef"},
		{"zażółć gęślą", 2, 5, "żół"}, {"a😀b", 1, 3, "😀"}, {"a😀b", -1, 4, "b"},
	}
	for _, c := range slices {
		if got := Slice(c.s, c.start, c.end); got != c.want {
			t.Errorf("Slice(%q, %d, %d) = %q, want %q", c.s, c.start, c.end, got, c.want)
		}
	}
}

func TestCompareUTF16(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"a", "b", -1}, {"B", "a", -1}, {"ab", "a", 1}, {"", "", 0}, {"ż", "ż", 0},
		{"z", "ż", -1},
		{"\xef\xbc\xa1", "\xf0\x9f\x98\x80", 1}, // U+FF21 > U+1F600 in UTF-16, < in UTF-8
		{"x\xee\x80\x80", "x\xf0\x9f\x98\x80", 1},
		{"\xed\xa0\xbd", "\xf0\x9f\x98\x80", -1}, // lone high surrogate is a prefix of the pair
	}
	for _, c := range cases {
		if got := CompareUTF16(c.a, c.b); got != c.want {
			t.Errorf("CompareUTF16(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := CompareUTF16(c.b, c.a); got != -c.want {
			t.Errorf("CompareUTF16(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestTrimAndPad(t *testing.T) {
	if got := Trim("\uFEFF\u00A0 \t x y \u2028\u3000\r\n"); got != "x y" {
		t.Errorf("Trim = %q", got)
	}
	if got := Trim("\u0085x\u0085"); got != "\u0085x\u0085" {
		t.Errorf("Trim removed U+0085, which JS keeps: %q", got)
	}
	if got := TrimStart("  a  "); got != "a  " {
		t.Errorf("TrimStart = %q", got)
	}
	if got := TrimEnd("  a  "); got != "  a" {
		t.Errorf("TrimEnd = %q", got)
	}
	pads := []struct{ got, want string }{
		{PadEnd("when", 8), "when    "},
		{PadStart("12", 6), "    12"},
		{PadStart("toolong", 3), "toolong"},
		{PadEnd("ż", 3), "ż  "},
		{PadStart("😀", 3), " 😀"},
		{PadEndWith("a", 6, "xy"), "axyxyx"},
		{PadStartWith("a", 3, ""), "a"},
	}
	for _, p := range pads {
		if p.got != p.want {
			t.Errorf("pad = %q, want %q", p.got, p.want)
		}
	}
}

func TestDecodeUTF8(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ok ż", "ok ż"},
		{"\xef\xbb\xbfbom", "\uFEFFbom"},
		{"a\xffb", "a\uFFFDb"},
		{"a\xe2\x82b", "a\uFFFDb"},             // truncated sequence: one replacement
		{"\xed\xa0\x80", "\uFFFD\uFFFD\uFFFD"}, // encoded surrogate: three
		{"\xf0\x9f\x98", "\uFFFD"},
		{"\xc0\xaf", "\uFFFD\uFFFD"},
	}
	for _, c := range cases {
		if got := DecodeUTF8([]byte(c.in)); got != c.want {
			t.Errorf("DecodeUTF8(%x) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	got := SplitLines("a\r\nb\nc\r")
	want := []string{"a", "b", "c\r"}
	if len(got) != len(want) {
		t.Fatalf("SplitLines = %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SplitLines = %q, want %q", got, want)
		}
	}
}

type localeOracle struct {
	Node       string  `json:"node"`
	ICU        string  `json:"icu"`
	Locale     string  `json:"locale"`
	AsciiOrder string  `json:"asciiOrder"`
	ASCII      string  `json:"ascii"`
	Pairs      [][]any `json:"pairs"`
}

func TestLocaleCompareMatchesNode(t *testing.T) {
	raw, err := os.ReadFile("testdata/locale.json")
	if err != nil {
		t.Fatal(err)
	}
	var o localeOracle
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	t.Logf("oracle: Node %s, ICU %s, %s", o.Node, o.ICU, o.Locale)

	bad := 0
	for a := 0; a < 128; a++ {
		for b := 0; b < 128; b++ {
			want := int(o.ASCII[a*128+b]) - '='
			if got := LocaleCompare(string(rune(a)), string(rune(b))); got != want {
				if bad++; bad < 10 {
					t.Errorf("LocaleCompare(%q, %q) = %d, Node %d", rune(a), rune(b), got, want)
				}
			}
		}
	}

	order := []byte(o.AsciiOrder)
	for i := 1; i < len(order); i++ {
		if LocaleCompare(string(order[i-1]), string(order[i])) >= 0 {
			t.Errorf("ASCII order broken at %q, %q", order[i-1], order[i])
		}
	}

	for _, p := range o.Pairs {
		a, b, want := p[0].(string), p[1].(string), int(p[2].(float64))
		if got := LocaleCompare(a, b); got != want {
			if bad++; bad < 20 {
				t.Errorf("LocaleCompare(%q, %q) = %d, Node %d", a, b, got, want)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d comparisons differ from Node", bad)
	}
}
