package weave

import (
	"regexp"
	"sort"
	"strings"
)

// GraphQL has one URL and many operations, so each root field is its own endpoint:
//
//	type Query { users(first: Int): [User!]! }    → QUERY /graphql/users
//	extend type Mutation { createUser(…): User }  → MUTATION /graphql/createUser
//	@Query(() => [User]) users() {}                → QUERY /graphql/users     (NestJS)
//	builder.queryField('users', …)                 → QUERY /graphql/users     (Pothos, Nexus)
//
// and a client document names the root fields it selects:
//
//	gql`query Users { users { id } }`              → call QUERY /graphql/users
//
// Client documents count only in a repo that declares a schema: a front end that
// talks to someone else's GraphQL API is not breaking anything of its own.

var (
	reGQLRootType  = regexp.MustCompile(`(?m)^\s*(?:extend\s+)?type\s+(Query|Mutation|Subscription)\b[^{]*\{`)
	reGQLSchemaMap = regexp.MustCompile(`schema\s*\{[^}]*\}`)
	reGQLNest      = jsRe(`@(Query|Mutation|Subscription)\s*\(([^)]*\)?[^)]*)\)\s*(?:@\w+(?:\([^)]*\))?\s*)*(?:async\s+)?([A-Za-z_$][\w$]*)\s*\(`)
	reGQLNestName  = regexp.MustCompile(`name\s*:\s*['"\x60]([A-Za-z_]\w*)['"\x60]`)
	reGQLCodeFirst = jsRe(`\b(query|mutation|subscription)Field\s*\(\s*['"\x60]([A-Za-z_]\w*)['"\x60]`)
	reGQLTag       = jsRe(`(?:\bgql|\bgraphql)\s*(?:\(\s*)?\x60`)
	reGQLOperation = regexp.MustCompile(`\b(query|mutation|subscription)\b\s*[A-Za-z_]?\w*\s*(?:\([^)]*\))?\s*(?:@\w+(?:\([^)]*\))?\s*)*\{`)
	reGQLAnonymous = regexp.MustCompile(`^\s*\{`)
)

func isGraphQLFile(rel string) bool {
	return strings.HasSuffix(rel, ".graphql") || strings.HasSuffix(rel, ".gql")
}

// gqlFields are the top-level field names inside the braces opening at text[open].
// Arguments, types and nested selections are skipped. In a schema a field is
// `name(args): Type`; in a document `alias: field` selects field.
func gqlFields(text string, open int, schema bool) (fields []string, at []int) {
	end := matchBrace(text, open)
	if end <= open {
		return nil, nil
	}
	depth := 0
	afterColon := false // schema: the next identifier is a type
	alias := false      // document: the next identifier is the aliased field
	i := open + 1
	for i < end {
		c := text[i]
		switch {
		case c == '#':
			for i < end && text[i] != '\n' {
				i++
			}
			continue
		case c == '"':
			if strings.HasPrefix(text[i:], `"""`) {
				k := strings.Index(text[i+3:end], `"""`)
				if k < 0 {
					return
				}
				i += k + 6
				continue
			}
			i++
			for i < end && text[i] != '"' && text[i] != '\n' {
				if text[i] == '\\' {
					i++
				}
				i++
			}
			i++
			continue
		case c == '{' || c == '(' || c == '[':
			if c == '[' && depth == 0 {
				afterColon = false // a list type: its name sits inside the brackets
			}
			depth++
		case c == '}' || c == ')' || c == ']':
			depth--
		case c == ':' && depth == 0:
			if schema {
				afterColon = true
			} else {
				alias = true
			}
		case depth == 0 && (c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'):
			start := i
			for i < end && isIdentChar(text[i]) {
				i++
			}
			name := text[start:i]
			prev := byte(0)
			if start > 0 {
				prev = text[start-1]
			}
			switch {
			case prev == '.' || prev == '@' || prev == '$':
				// spread, directive or variable
			case schema && afterColon:
				afterColon = false // the field's type
			case !schema && alias:
				alias = false
				fields[len(fields)-1] = name
			case name == "on" || name == "fragment" || name == "__typename" || name == "implements":
			default:
				fields = append(fields, name)
				at = append(at, start)
			}
			continue
		}
		i++
	}
	return fields, at
}

// gqlDocuments are the GraphQL texts of a file: the whole file for .graphql, the
// gql“ / graphql(`…`) template bodies otherwise. Offsets are into f.Text.
func gqlDocuments(f *SourceFile) [][2]int {
	if isGraphQLFile(f.Rel) {
		return [][2]int{{0, len(f.Text)}}
	}
	var out [][2]int
	for _, m := range reGQLTag.FindAllStringIndex(f.Text, -1) {
		start := m[1]
		end := strings.IndexByte(f.Text[start:], '`')
		if end < 0 {
			break
		}
		out = append(out, [2]int{start, start + end})
	}
	return out
}

func gqlKind(word string) string {
	switch strings.ToLower(word) {
	case "mutation":
		return "MUTATION"
	case "subscription":
		return "SUBSCRIPTION"
	}
	return "QUERY"
}

// ExtractGraphQL reads root fields from schemas and resolvers as endpoints, and
// the operations of client documents as calls.
func ExtractGraphQL(files []SourceFile, b *Builder) ([]Endpoint, []Call) {
	var out []Endpoint
	seen := map[string]bool{}
	mod := "graphql"
	var modFiles []string
	add := func(f *SourceFile, kind, field string, at int, framework string) {
		path := "/graphql/" + field
		eid := EndpointID(kind, path)
		if seen[eid] {
			return
		}
		seen[eid] = true
		line := lineAt(f.Text, at)
		b.Node(&GNode{ID: eid, Kind: KindEndpoint, Name: kind + " " + path, File: Ptr(f.Rel), Line: Ptr(line), Meta: strMeta("module", mod, "desc", strings.ToLower(kind)+" "+field, "framework", framework)})
		b.Edge(eid, FileID(f.Rel), "defined_in")
		out = append(out, Endpoint{kind, path, f.Rel, line, mod, eid})
		modFiles = append(modFiles, f.Rel)
	}
	type doc struct {
		f          *SourceFile
		start, end int
	}
	var docs []doc
	for i := range files {
		f := &files[i]
		gql := isGraphQLFile(f.Rel)
		if !gql && (!isCode(f.Rel) || reTestOrSpec.MatchString(f.Rel)) {
			continue
		}
		if !gql && !strings.Contains(f.Text, "gql") && !strings.Contains(f.Text, "raphql") &&
			!strings.Contains(f.Text, "Field(") && !strings.Contains(f.Text, "@Query") && !strings.Contains(f.Text, "@Mutation") {
			continue
		}
		// schema-first: type Query { … } in .graphql files and typeDefs strings
		for _, d := range gqlDocuments(f) {
			text := f.Text[:d[1]]
			for _, m := range reGQLRootType.FindAllStringSubmatchIndex(text[d[0]:], -1) {
				open := d[0] + m[1] - 1
				kind := gqlKind(text[d[0]+m[2] : d[0]+m[3]])
				fields, at := gqlFields(text, open, true)
				for k, name := range fields {
					add(f, kind, name, at[k], "graphql")
				}
			}
			docs = append(docs, doc{f, d[0], d[1]})
		}
		if gql {
			continue
		}
		// code-first: NestJS resolvers, Pothos / Nexus fields
		if strings.Contains(f.Text, "@Query") || strings.Contains(f.Text, "@Mutation") || strings.Contains(f.Text, "@Subscription") {
			for _, m := range reGQLNest.FindAllStringSubmatchIndex(f.Text, -1) {
				name := f.Text[m[6]:m[7]]
				if n := reGQLNestName.FindStringSubmatch(f.Text[m[4]:m[5]]); n != nil {
					name = n[1]
				}
				add(f, gqlKind(f.Text[m[2]:m[3]]), name, m[0], "nest-graphql")
			}
		}
		for _, m := range reGQLCodeFirst.FindAllStringSubmatchIndex(f.Text, -1) {
			add(f, gqlKind(f.Text[m[2]:m[3]]), f.Text[m[4]:m[5]], m[0], "graphql")
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	b.Node(&GNode{ID: ModuleID(mod), Kind: KindModule, Name: mod, File: Ptr(modFiles[0])})
	sort.Strings(modFiles)
	for i, rel := range modFiles {
		if i == 0 || rel != modFiles[i-1] {
			b.Edge(ModuleID(mod), FileID(rel), "contains")
		}
	}
	for _, e := range out {
		b.Edge(e.ID, ModuleID(mod), "in_module")
	}

	// client operations
	var calls []Call
	for _, d := range docs {
		body := d.f.Text[d.start:d.end]
		if reGQLRootType.MatchString(body) || reGQLSchemaMap.MatchString(body) {
			continue // a schema, not an operation
		}
		ops := reGQLOperation.FindAllStringSubmatchIndex(body, -1)
		if len(ops) == 0 && reGQLAnonymous.MatchString(body) {
			ops = [][]int{{0, strings.IndexByte(body, '{') + 1, -1, -1}}
		}
		for _, m := range ops {
			kind := "QUERY"
			if m[2] >= 0 {
				kind = gqlKind(body[m[2]:m[3]])
			}
			open := d.start + m[1] - 1
			fields, at := gqlFields(d.f.Text[:d.end], open, false)
			for k, name := range fields {
				calls = append(calls, Call{kind, "/graphql/" + name, d.f.Rel, lineAt(d.f.Text, at[k]), false})
			}
		}
	}
	return out, calls
}
