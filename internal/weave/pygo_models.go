package weave

import (
	"path"
	"regexp"
	"strings"
)

// Python and Go declare tables in code:
//
//	class Article(db.Model): __tablename__ = 'article'      # SQLAlchemy / Flask-SQLAlchemy
//	class Article(models.Model): class Meta: db_table = …   # Django
//	type ArticleModel struct { gorm.Model … }                // GORM
//
// Each becomes a model and its table, the same nodes a Prisma model makes, and a
// module that queries one (Article.query, Article.objects, select(Article),
// db.Find(&ArticleModel{})) gets the same `uses` edge.

var (
	rePyClass     = regexp.MustCompile(`(?m)^class\s+([A-Za-z_]\w*)\s*\(([^)]*)\)\s*:`)
	rePyTableName = regexp.MustCompile(`(?m)^[ \t]+__tablename__\s*=\s*[rRuU]?["']([^"']+)["']`)
	rePyDbTable   = regexp.MustCompile(`(?m)^[ \t]+db_table\s*=\s*[rRuU]?["']([^"']+)["']`)
	rePyAbstract  = regexp.MustCompile(`(?m)^[ \t]+(?:abstract\s*=\s*True|__abstract__\s*=\s*True)`)
	rePyIdent     = regexp.MustCompile(`[A-Za-z_]\w*`)
	reGoStruct    = regexp.MustCompile(`(?m)^type\s+([A-Z]\w*)\s+struct\s*\{`)
	reGoTableName = regexp.MustCompile(`func\s*\(\s*\w*\s*\*?([A-Z]\w*)\s*\)\s*TableName\s*\(\s*\)\s*string\s*\{\s*return\s*"([^"]+)"`)
	reGoIdent     = regexp.MustCompile(`(?:&|\[\]\*?|\(\s*)([A-Z]\w*)\s*\{|\[\]\*?([A-Z]\w*)\b`)
)

// pyModelBase reports whether a base class list names an ORM model base. Pydantic's
// BaseModel and marshmallow schemas are data shapes, not tables.
func pyModelBase(bases string, known map[string]bool) bool {
	for _, b := range strings.Split(bases, ",") {
		b = strings.TrimSpace(b)
		if i := strings.LastIndex(b, "."); i >= 0 {
			b = b[i+1:]
		}
		switch {
		case b == "BaseModel" || strings.HasSuffix(b, "Schema"):
		case known[b], b == "Model", b == "Base", b == "DeclarativeBase":
			return true
		}
	}
	return false
}

// classBody is the indented block under a class line, up to the next top-level line.
func classBody(t string, from int) string {
	end := len(t)
	for i := from; i < len(t); i++ {
		if t[i] == '\n' && i+1 < len(t) && t[i+1] != ' ' && t[i+1] != '\t' && t[i+1] != '\n' && t[i+1] != '#' {
			end = i
			break
		}
	}
	return t[from:end]
}

func pluralTable(s string) string {
	switch {
	case strings.HasSuffix(s, "y") && !strings.HasSuffix(s, "ey"):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "ch"):
		return s + "es"
	}
	return s + "s"
}

// ExtractPythonGoModels reads SQLAlchemy, Django and GORM models, and which modules use them.
func ExtractPythonGoModels(files []SourceFile, b *Builder) []Model {
	var out []Model
	pyModels := map[string]bool{}
	goModels := map[string]bool{}

	// Python: a class is a model if its base is a model base or a model already
	// found, so a TimestampedModel(models.Model) mixin carries its subclasses.
	type pyClass struct {
		f           *SourceFile
		name, bases string
		at, body    int
	}
	var classes []pyClass
	for i := range files {
		f := &files[i]
		if !isPython(f.Rel) || isPyOrGoTest(f.Rel) {
			continue
		}
		if !strings.Contains(f.Text, "sqlalchemy") && !strings.Contains(f.Text, "django.db") && !strings.Contains(f.Text, "Model") {
			continue
		}
		t := pyCode(f.Text)
		for _, m := range rePyClass.FindAllStringSubmatchIndex(t, -1) {
			classes = append(classes, pyClass{f, t[m[2]:m[3]], t[m[4]:m[5]], m[0], m[1]})
		}
	}
	known := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, c := range classes {
			if !known[c.name] && pyModelBase(c.bases, known) {
				// a class named Model is everywhere; it counts where the file is ORM code
				orm := strings.Contains(c.f.Text, "sqlalchemy") || strings.Contains(c.f.Text, "django.db") ||
					strings.Contains(c.f.Text, "Column(") || strings.Contains(c.bases, "db.Model") || anyKnown(c.bases, known)
				if !orm {
					continue
				}
				known[c.name] = true
				changed = true
			}
		}
	}
	for _, c := range classes {
		if !known[c.name] {
			continue
		}
		body := classBody(c.f.Text, c.body)
		if rePyAbstract.MatchString(body) {
			continue
		}
		django := strings.Contains(c.f.Text, "django.db") || strings.Contains(c.bases, "models.")
		table := ""
		if m := rePyTableName.FindStringSubmatch(body); m != nil {
			table = m[1]
		} else if m := rePyDbTable.FindStringSubmatch(body); m != nil {
			table = m[1]
		} else if django {
			table = path.Base(path.Dir(c.f.Rel)) + "_" + strings.ToLower(c.name)
		} else if strings.Contains(body, "__table__") || strings.Contains(body, "= Column(") || strings.Contains(body, "db.Column(") || strings.Contains(body, "mapped_column(") || strings.Contains(body, "Column(") {
			table = snake(c.name)
		} else {
			continue // a mixin or a plain base: no columns, no table
		}
		orm := "sqlalchemy"
		if django {
			orm = "django"
		}
		out = append(out, ormModel(b, c.f, c.name, table, lineAt(c.f.Text, c.at), orm))
		pyModels[c.name] = true
	}

	// Go: a struct that embeds gorm.Model or carries gorm tags.
	tableOf := map[string]string{}
	for i := range files {
		f := &files[i]
		if isGo(f.Rel) && strings.Contains(f.Text, "TableName") {
			for _, m := range reGoTableName.FindAllStringSubmatch(f.Text, -1) {
				tableOf[m[1]] = m[2]
			}
		}
	}
	for i := range files {
		f := &files[i]
		if !isGo(f.Rel) || isPyOrGoTest(f.Rel) || !strings.Contains(f.Text, "gorm") {
			continue
		}
		for _, m := range reGoStruct.FindAllStringSubmatchIndex(f.Text, -1) {
			open := m[1] - 1
			body := f.Text[open:matchBrace(f.Text, open)]
			if !strings.Contains(body, "gorm.Model") && !strings.Contains(body, "gorm:\"") {
				continue
			}
			name := f.Text[m[2]:m[3]]
			table := tableOf[name]
			if table == "" {
				table = pluralTable(snake(name))
			}
			out = append(out, ormModel(b, f, name, table, lineAt(f.Text, m[0]), "gorm"))
			goModels[name] = true
		}
	}
	if len(pyModels)+len(goModels) == 0 {
		return out
	}

	for i := range files {
		f := &files[i]
		if isPyOrGoTest(f.Rel) {
			continue
		}
		var mod string
		var hits []string
		switch {
		case isPython(f.Rel) && len(pyModels) > 0:
			mod = pyModule(f.Rel)
			t := pyCode(f.Text)
			for name := range pyModels {
				if strings.Contains(t, name+".query") || strings.Contains(t, name+".objects") ||
					regexp.MustCompile(`(?:select|query|get_object_or_404|get_list_or_404)\(\s*`+name+`\b|\bmodel\s*=\s*`+name+`\b`).MatchString(t) {
					hits = append(hits, name)
				}
			}
		case isGo(f.Rel) && len(goModels) > 0 && strings.Contains(f.Text, "gorm"):
			mod = path.Base(path.Dir(f.Rel))
			for _, m := range reGoIdent.FindAllStringSubmatch(f.Text, -1) {
				if n := m[1] + m[2]; goModels[n] {
					hits = append(hits, n)
				}
			}
		}
		if mod == "" || mod == "." {
			continue
		}
		seen := map[string]bool{}
		for _, h := range hits {
			if !seen[h] {
				seen[h] = true
				b.Use(mod, f.Rel, h)
			}
		}
	}
	return out
}

func anyKnown(bases string, known map[string]bool) bool {
	for _, b := range rePyIdent.FindAllString(bases, -1) {
		if known[b] {
			return true
		}
	}
	return false
}
