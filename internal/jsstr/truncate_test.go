package jsstr

import (
	"strings"
	"testing"
)

func TestTruncateWords(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short description", 40, "short description"},
		{"exactly ten", 11, "exactly ten"},
		{"cards with a 4px status rail and mono task numbers; board", 40, "cards with a 4px status rail and mono…"},
		{"no spaces at all here", 6, "no…"},
	}
	for _, c := range cases {
		got := TruncateWords(c.in, c.max)
		if got != c.want {
			t.Errorf("TruncateWords(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
		if Len(got) > c.max+1 && got != c.in {
			t.Errorf("TruncateWords(%q, %d) = %q: longer than max + ellipsis", c.in, c.max, got)
		}
		if got != c.in && !strings.HasSuffix(got, "…") {
			t.Errorf("TruncateWords(%q, %d) = %q: a cut is not marked", c.in, c.max, got)
		}
	}
}
