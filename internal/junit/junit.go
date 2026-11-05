// Package junit is the Go port of src/junit.ts: JUnit XML is the one test report
// every runner can write, and reading it proves a requirement is verified.
package junit

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Status is a test result.
type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
	Skip Status = "skip"
)

// Case is one test case from a report.
type Case struct {
	Name   string
	Suite  string
	File   *string
	Status Status
}

var (
	reqPattern  = regexp.MustCompile(`\b((?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*)\b`)
	commonJUnit = []string{
		".weave/junit.xml",
		"junit.xml",
		"test-results/junit.xml",
		"test-results.xml",
		"coverage/junit.xml",
		"reports/junit.xml",
		"packages/backend/junit.xml",
		"packages/web/junit.xml",
	}
)

// FindJUnitFile locates the newest JUnit report the project already produced.
// The second result is false when there is none.
func FindJUnitFile(root string) (string, bool) {
	type found struct {
		path  string
		mtime int64
	}
	hits := make([]found, 0, len(commonJUnit))
	for _, rel := range commonJUnit {
		p := filepath.Join(root, rel)
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		hits = append(hits, found{path: p, mtime: info.ModTime().UnixNano()})
	}
	if len(hits) == 0 {
		return "", false
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].mtime > hits[j].mtime })
	return hits[0].path, true
}

// Load reads the newest JUnit report under root. It returns nil when the project
// has written none, so a gate can say the evidence was not checked.
func Load(root string) []Case {
	p, ok := FindJUnitFile(root)
	if !ok {
		return nil
	}
	text, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return Parse(string(text))
}

// Parse reads the test cases out of a JUnit document. A malformed document keeps
// the cases read before the error, rather than failing the command.
func Parse(text string) []Case {
	out := make([]Case, 0)
	dec := xml.NewDecoder(strings.NewReader(protectEntities(text)))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "testcase" {
			continue
		}
		c := Case{Status: Pass}
		className, suiteName := "", ""
		hasClass, hasSuite := false, false
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "name":
				c.Name = a.Value
			case "classname":
				className, hasClass = a.Value, true
			case "suite":
				suiteName, hasSuite = a.Value, true
			case "file":
				f := a.Value
				c.File = &f
			}
		}
		if hasClass {
			c.Suite = className
		} else if hasSuite {
			c.Suite = suiteName
		}
		depth := 1
		for depth > 0 {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			switch e := tok.(type) {
			case xml.StartElement:
				depth++
				switch e.Name.Local {
				case "failure", "error":
					c.Status = Fail
				case "skipped":
					if c.Status != Fail {
						c.Status = Skip
					}
				}
			case xml.EndElement:
				depth--
			}
		}
		out = append(out, c)
	}
	return out
}

// ReqsInCase is the requirement ids a test case points at. The TypeScript
// searches the case name and suite only; Case.File is not scanned.
func ReqsInCase(c Case) []string {
	out := make([]string, 0)
	for _, m := range reqPattern.FindAllStringSubmatch(c.Name+" "+c.Suite, -1) {
		out = append(out, m[1])
	}
	return out
}

// protectEntities keeps encoding/xml from decoding more than src/junit.ts does.
// Its decode knows only the five named entities; a numeric reference, an unknown
// entity or a bare & stays literal. Escaping every other & before the XML parser
// sees it reproduces that exactly, and makes those reports parse instead of fail.
func protectEntities(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		if isNamedEntity(s[i:]) {
			b.WriteByte('&')
		} else {
			b.WriteString("&amp;")
		}
	}
	return b.String()
}

func isNamedEntity(s string) bool {
	for _, e := range [...]string{"&lt;", "&gt;", "&quot;", "&apos;", "&amp;"} {
		if strings.HasPrefix(s, e) {
			return true
		}
	}
	return false
}
