package version

import "testing"

func TestFromModule(t *testing.T) {
	for in, want := range map[string]string{
		"v0.2.0":                               "0.2.0",
		"v0.2.1-0.20261106120000-abcdef123456": "0.2.1-0.20261106120000-abcdef123456",
		"(devel)":                              "",
		"":                                     "",
	} {
		if got := fromModule(in); got != want {
			t.Errorf("fromModule(%q) = %q, want %q", in, got, want)
		}
	}
}
