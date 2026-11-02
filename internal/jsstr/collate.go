package jsstr

// weight is one character's place in Node's default collation (ICU root order
// for en-US, punctuation non-ignorable): a primary class, then its rank inside
// that class at the accent and the case level.
type weight struct {
	p    uint32
	s, t uint8
}

// fallbackBase puts characters outside the table after every known one.
const fallbackBase = 1 << 16

// LocaleCompare is String.prototype.localeCompare with Node's default locale,
// returning -1, 0 or 1. It compares all primaries first, then accents, then case,
// which is how ICU orders "a" < "A" < "b" and "ab" < "Ab" < "ac".
//
// The table covers ASCII and the Polish letters, generated from Node by
// testdata/gen.mjs. Any other character is a documented divergence: it sorts after
// all of them, by code point, with no accent or case folding.
func LocaleCompare(a, b string) int {
	if a == b {
		return 0
	}
	for level := 0; level < 3; level++ {
		if c := compareLevel(a, b, level); c != 0 {
			return c
		}
	}
	return 0
}

func compareLevel(a, b string, level int) int {
	i, j := 0, 0
	for {
		wa, ni, oka := nextWeight(a, i)
		wb, nj, okb := nextWeight(b, j)
		switch {
		case !oka && !okb:
			return 0
		case !oka:
			return -1
		case !okb:
			return 1
		}
		x, y := wa.p, wb.p
		if level == 1 {
			x, y = uint32(wa.s), uint32(wb.s)
		} else if level == 2 {
			x, y = uint32(wa.t), uint32(wb.t)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
		i, j = ni, nj
	}
}

// nextWeight returns the weight of the first non-ignorable character at or after s[i].
func nextWeight(s string, i int) (weight, int, bool) {
	for i < len(s) {
		r, w, _ := unitAt(s, i)
		i += w
		var wt weight
		if r < 128 {
			wt = asciiWeights[r]
		} else if x, ok := extraWeights[r]; ok {
			wt = x
		} else {
			wt = weight{p: fallbackBase + uint32(r)}
		}
		if wt.p != 0 {
			return wt, i, true
		}
	}
	return weight{}, i, false
}
