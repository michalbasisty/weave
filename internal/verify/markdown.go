package verify

import (
	"fmt"
	"strings"
)

// Marker starts every PR comment weave writes, so the action edits its own
// comment instead of adding a new one on every push.
const Marker = "<!-- weave-check -->"

// Markdown renders the result as a pull-request comment: a verdict, what broke
// and which screens it reaches, then what the change touches.
func (r Result) Markdown() string {
	var b strings.Builder
	b.WriteString(Marker + "\n")
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "### ❌ weave check: %d problem(s)\n\n", len(r.Errors))
	} else {
		b.WriteString("### ✅ weave check passed\n\n")
	}
	fmt.Fprintf(&b, "_%s_\n\n", mdEscape(r.Scope))

	if len(r.Broken) > 0 {
		b.WriteString("**Removed endpoints that are still called**\n\n| endpoint | called from | screens |\n|---|---|---|\n")
		for i, x := range r.Broken {
			if i == 20 {
				fmt.Fprintf(&b, "| … %d more | | |\n", len(r.Broken)-20)
				break
			}
			screens := "—"
			if len(x.Screens) > 0 {
				screens = codeList(head(x.Screens, 5))
				if len(x.Screens) > 5 {
					screens += fmt.Sprintf(" +%d", len(x.Screens)-5)
				}
			}
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", x.Endpoint, x.Caller, screens)
		}
		b.WriteString("\n")
	}
	var other []string
	for _, e := range r.Errors {
		if !strings.HasPrefix(e, "removed endpoint still called") {
			other = append(other, e)
		}
	}
	if len(other) > 0 {
		for _, e := range other {
			fmt.Fprintf(&b, "- ❌ %s\n", mdEscape(e))
		}
		b.WriteString("\n")
	}
	if len(r.Warnings) > 0 {
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "- ⚠️ %s\n", mdEscape(w))
		}
		b.WriteString("\n")
	}
	if len(r.Pages)+len(r.Endpoints) > 0 {
		b.WriteString("<details><summary>What this change touches</summary>\n\n")
		if len(r.Pages) > 0 {
			fmt.Fprintf(&b, "**Screens (%d):** %s\n\n", len(r.Pages), capped(r.Pages, 30))
		}
		if len(r.Endpoints) > 0 {
			fmt.Fprintf(&b, "**Endpoints (%d):** %s\n\n", len(r.Endpoints), capped(r.Endpoints, 30))
		}
		b.WriteString("</details>\n\n")
	}
	if len(r.Commands) > 0 {
		b.WriteString("**Tests to run**\n\n```sh\n" + strings.Join(r.Commands, "\n") + "\n```\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func codeList(xs []string) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = "`" + x + "`"
	}
	return strings.Join(out, ", ")
}

func capped(xs []string, n int) string {
	s := codeList(head(xs, n))
	if len(xs) > n {
		s += fmt.Sprintf(" and %d more", len(xs)-n)
	}
	return s
}

func mdEscape(s string) string {
	return strings.NewReplacer("|", "\\|", "<", "&lt;", ">", "&gt;").Replace(s)
}
