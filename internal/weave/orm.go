package weave

import (
	"strings"
	"unicode"
)

// Drizzle and TypeORM declare tables in TypeScript instead of a schema file:
//
//	export const users = pgTable('users', { … })            // Drizzle
//	@Entity('users') export class User { … }                 // TypeORM
//
// Each becomes a model and the table it maps to, the same nodes a Prisma model
// makes, and a module that queries one (`db.select().from(users)`,
// `@InjectRepository(User)`) gets the same `uses` edge.

var (
	reDrizzleTable = jsRe(`(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*(?:pg|mysql|sqlite|singlestore)Table\s*\(\s*['"\x60]([^'"\x60]+)['"\x60]`)
	reDrizzleUse   = jsRe(`\.(?:from|insert|update|delete|innerJoin|leftJoin|rightJoin|fullJoin)\s*\(\s*([A-Za-z_$][\w$]*)\s*[,)]`)
	reDrizzleQuery = jsRe(`\.query\.([A-Za-z_$][\w$]*)\.`)
	reEntity       = jsRe(`@Entity\s*\(\s*(?:['"\x60]([^'"\x60]+)['"\x60]|\{[^}]*?name\s*:\s*['"\x60]([^'"\x60]+)['"\x60][^}]*\})?[^)]*\)\s*(?:@\w+(?:\([^)]*\))?\s*)*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`)
	reEntityUse    = jsRe(`(?:Repository\s*<\s*|getRepository\s*\(\s*|InjectRepository\s*\(\s*|\.(?:find|findOne|findOneBy|findBy|findAndCount|count|save|insert|update|delete|remove|createQueryBuilder)\s*\(\s*)([A-Z][\w$]*)`)
)

// snake is TypeORM's default table name: the class name, lower snake case.
func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ExtractORMModels reads Drizzle tables and TypeORM entities, and which modules use them.
func ExtractORMModels(files []SourceFile, b *Builder) []Model {
	var out []Model
	drizzle := map[string]string{} // variable → model
	entity := map[string]string{}  // class → model
	for i := range files {
		f := &files[i]
		if !isCode(f.Rel) {
			continue
		}
		if strings.Contains(f.Text, "Table(") {
			for _, m := range reDrizzleTable.FindAllStringSubmatchIndex(f.Text, -1) {
				name, table := f.Text[m[2]:m[3]], f.Text[m[4]:m[5]]
				line := lineAt(f.Text, m[0])
				out = append(out, ormModel(b, f, name, table, line, "drizzle"))
				drizzle[name] = name
			}
		}
		if strings.Contains(f.Text, "@Entity") {
			for _, m := range reEntity.FindAllStringSubmatchIndex(f.Text, -1) {
				name := f.Text[m[6]:m[7]]
				table := snake(name)
				if m[2] >= 0 {
					table = f.Text[m[2]:m[3]]
				} else if m[4] >= 0 {
					table = f.Text[m[4]:m[5]]
				}
				line := lineAt(f.Text, m[0])
				out = append(out, ormModel(b, f, name, table, line, "typeorm"))
				entity[name] = name
			}
		}
	}
	if len(drizzle)+len(entity) == 0 {
		return out
	}
	for i := range files {
		f := &files[i]
		if !isCode(f.Rel) || reTestOrSpec.MatchString(f.Rel) {
			continue
		}
		mod := moduleOf(f.Rel)
		if mod == "" {
			continue
		}
		seen := map[string]bool{}
		use := func(model string) {
			if model != "" && !seen[model] {
				seen[model] = true
				b.Edge(ModuleID(mod), ModelID(model), "uses")
			}
		}
		if len(drizzle) > 0 {
			for _, m := range reDrizzleUse.FindAllStringSubmatch(f.Text, -1) {
				use(drizzle[m[1]])
			}
			for _, m := range reDrizzleQuery.FindAllStringSubmatch(f.Text, -1) {
				use(drizzle[m[1]])
			}
		}
		if len(entity) > 0 {
			for _, m := range reEntityUse.FindAllStringSubmatch(f.Text, -1) {
				use(entity[m[1]])
			}
		}
	}
	return out
}

func ormModel(b *Builder, f *SourceFile, name, table string, line int, orm string) Model {
	mid := ModelID(name)
	desc := CommentAbove(f.Lines, line, 220)
	b.Node(&GNode{ID: mid, Kind: KindModel, Name: name, File: Ptr(f.Rel), Line: Ptr(line), Meta: strMeta("desc", desc, "orm", orm)})
	b.Node(&GNode{ID: TableID(table), Kind: KindTable, Name: table, File: Ptr(f.Rel), Meta: strMeta("desc", desc)})
	b.Edge(mid, TableID(table), "maps_to")
	return Model{name, table, f.Rel, line, nil}
}
