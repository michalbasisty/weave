package server

import (
	"html"
	"regexp"
	"strings"
)

// renderSpecMD is a small Markdown reader for the subset the spec files use: ATX
// headings, paragraphs, bullet and ordered lists with wrapped lines, pipe tables with
// a header separator, inline code and bold. Everything is HTML-escaped before a tag is
// added, so a spec can contain <script> without becoming one.
func renderSpecMD(src string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n"), "\n")
	var b strings.Builder
	i := 0
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			i++
			continue
		}
		if level, text := heading(t); level > 0 {
			tag := "h" + string(rune('0'+level))
			b.WriteString("<" + tag + ">" + inlineMD(text) + "</" + tag + ">")
			i++
			continue
		}
		if strings.HasPrefix(t, "|") {
			var rows [][]string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				rows = append(rows, tableCells(strings.TrimSpace(lines[i])))
				i++
			}
			b.WriteString(renderTable(rows))
			continue
		}
		if _, ok := listItem(t); ok {
			b.WriteString(renderList(lines, &i))
			continue
		}
		para := t
		i++
		for i < len(lines) {
			next := strings.TrimSpace(lines[i])
			if next == "" || startsBlock(next) {
				break
			}
			para += " " + next
			i++
		}
		b.WriteString("<p>" + inlineMD(para) + "</p>")
	}
	return b.String()
}

func heading(t string) (int, string) {
	n := 0
	for n < len(t) && n < 6 && t[n] == '#' {
		n++
	}
	if n == 0 || n >= len(t) || t[n] != ' ' {
		return 0, ""
	}
	return n, strings.TrimSpace(t[n+1:])
}

func startsBlock(t string) bool {
	if t == "" {
		return true
	}
	if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "|") || strings.HasPrefix(t, "```") || strings.HasPrefix(t, ">") {
		return true
	}
	if _, ok := listItem(t); ok {
		return true
	}
	return false
}

var reOrdered = regexp.MustCompile(`^\d+\.\s`)

func listItem(t string) (string, bool) {
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
		return t[2:], true
	}
	if reOrdered.MatchString(t) {
		return reOrdered.ReplaceAllString(t, ""), true
	}
	return "", false
}

func renderList(lines []string, i *int) string {
	ordered := reOrdered.MatchString(strings.TrimSpace(lines[*i]))
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	var b strings.Builder
	b.WriteString("<" + tag + ">")
	for *i < len(lines) {
		t := strings.TrimSpace(lines[*i])
		item, ok := listItem(t)
		if !ok {
			break
		}
		*i++
		for *i < len(lines) {
			raw := lines[*i]
			next := strings.TrimSpace(raw)
			if next == "" || !strings.HasPrefix(raw, " ") && !strings.HasPrefix(raw, "\t") || startsBlock(next) {
				break
			}
			item += " " + next
			*i++
		}
		b.WriteString("<li>" + inlineMD(strings.TrimSpace(item)) + "</li>")
		// A blank line between items is not the end of the list.
		for *i < len(lines) && strings.TrimSpace(lines[*i]) == "" {
			*i++
		}
		if *i < len(lines) {
			if _, ok := listItem(strings.TrimSpace(lines[*i])); !ok {
				break
			}
		}
	}
	b.WriteString("</" + tag + ">")
	return b.String()
}

func tableCells(t string) []string {
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	raw := strings.Split(t, "|")
	out := make([]string, len(raw))
	for i, c := range raw {
		out[i] = strings.TrimSpace(c)
	}
	return out
}

var reTableSep = regexp.MustCompile(`^:?-{3,}:?$`)

func isSeparator(row []string) bool {
	if len(row) == 0 {
		return false
	}
	for _, c := range row {
		if !reTableSep.MatchString(strings.TrimSpace(c)) {
			return false
		}
	}
	return true
}

func renderTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	body := rows
	var b strings.Builder
	b.WriteString(`<div class="mdtable"><table>`)
	if len(rows) > 1 && isSeparator(rows[1]) {
		b.WriteString("<thead><tr>")
		for _, c := range rows[0] {
			b.WriteString("<th>" + inlineMD(c) + "</th>")
		}
		b.WriteString("</tr></thead>")
		body = rows[2:]
	}
	b.WriteString("<tbody>")
	for _, row := range body {
		b.WriteString("<tr>")
		for _, c := range row {
			b.WriteString("<td>" + inlineMD(c) + "</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table></div>")
	return b.String()
}

var (
	reMDCode = regexp.MustCompile("`([^`]+)`")
	reMDBold = regexp.MustCompile(`\*\*([^*]+)\*\*`)
)

func inlineMD(s string) string {
	s = html.EscapeString(s)
	s = reMDCode.ReplaceAllString(s, "<code>$1</code>")
	s = reMDBold.ReplaceAllString(s, "<strong>$1</strong>")
	return s
}

// specTitle is the first level-one heading, which every capability spec starts with.
func specTitle(md string) string {
	for _, raw := range strings.Split(md, "\n") {
		if level, text := heading(strings.TrimSpace(raw)); level == 1 {
			return text
		}
	}
	return ""
}
