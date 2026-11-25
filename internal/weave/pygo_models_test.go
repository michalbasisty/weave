package weave

import (
	"sort"
	"strings"
	"testing"
)

func TestPythonAndGoModels(t *testing.T) {
	files := map[string]string{
		// Flask-SQLAlchemy through an alias, a mixin without columns, a default table name
		"app/database.py": "from .extensions import db\nModel = db.Model\nclass SurrogatePK(object):\n    id = 1\n",
		"app/articles/models.py": `from app.database import Model, SurrogatePK, db, Column
class Article(SurrogatePK, Model):
    __tablename__ = 'article'
    title = Column(db.String(100))
class ArticleTag(Model):
    name = Column(db.String(10))
`,
		"app/articles/views.py": "from .models import Article\ndef list(): return Article.query.all()\n",
		// Django: an abstract base carries its subclasses, Meta.db_table wins
		"shop/core/models.py": `from django.db import models
class Timestamped(models.Model):
    class Meta:
        abstract = True
`,
		"shop/orders/models.py": `from django.db import models
from shop.core.models import Timestamped
class Order(Timestamped):
    total = models.IntegerField()
class Line(models.Model):
    class Meta:
        db_table = 'order_lines'
`,
		"shop/orders/serializers.py": "class OrderSerializer:\n    class Meta:\n        model = Order\n",
		// Pydantic is a shape, not a table
		"api/schemas.py": "from pydantic import BaseModel\nclass Item(BaseModel):\n    name: str\n",
		// GORM: embedded gorm.Model, a TableName override, a struct that is no model
		"backend/users/models.go":      "package users\nimport \"gorm.io/gorm\"\ntype UserModel struct {\n\tgorm.Model\n\tName string\n}\ntype Follow struct {\n\tID uint `gorm:\"primaryKey\"`\n}\nfunc (Follow) TableName() string { return \"follows\" }\ntype Form struct { Name string }\nfunc Find(db *gorm.DB) { db.First(&UserModel{}) }\n",
		"backend/users/models_test.go": "package users\nimport \"gorm.io/gorm\"\ntype Fake struct { gorm.Model }\n",
		"tests/factories.py":           "from app.articles.models import Article\nArticle.query.first()\n",
	}
	var src []SourceFile
	for rel, text := range files {
		src = append(src, SourceFile{Rel: rel, Text: text, Lines: strings.Split(text, "\n")})
	}
	sort.Slice(src, func(i, j int) bool { return src[i].Rel < src[j].Rel })
	b := NewBuilder()
	var got []string
	for _, m := range ExtractPythonGoModels(src, b) {
		got = append(got, m.Name+"="+m.Table)
	}
	sort.Strings(got)
	wantNames(t, got, "Article=article", "ArticleTag=article_tag", "Order=orders_order", "Line=order_lines", "UserModel=user_models", "Follow=follows")

	var uses []string
	for _, e := range b.edges {
		if e.Kind == "uses" {
			uses = append(uses, e.From+" "+e.To)
		}
	}
	sort.Strings(uses)
	want := []string{ModuleID("articles") + " " + ModelID("Article"), ModuleID("orders") + " " + ModelID("Order"), ModuleID("users") + " " + ModelID("UserModel")}
	sort.Strings(want)
	if strings.Join(uses, "\n") != strings.Join(want, "\n") {
		t.Fatalf("uses:\n  %s\nwant\n  %s", strings.Join(uses, "\n  "), strings.Join(want, "\n  "))
	}
}
