package jsstr

import "strings"

// TruncateWords is Slice(s, 0, max) that ends on a word boundary and marks the
// cut with an ellipsis, so a description never stops mid-word ("… task numbers; bo").
// A string that already fits is returned unchanged.
func TruncateWords(s string, max int) string {
	if Len(s) <= max {
		return s
	}
	cut := Slice(s, 0, max)
	if i := strings.LastIndexAny(cut, " \t"); i > 0 {
		cut = strings.TrimRight(cut[:i], " \t,;:")
	}
	return strings.TrimRight(cut, " \t") + "…"
}
