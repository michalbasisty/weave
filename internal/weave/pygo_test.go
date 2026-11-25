package weave

import (
	"sort"
	"strings"
	"testing"
)

func pyGoNames(t *testing.T, files map[string]string) []string {
	t.Helper()
	var src []SourceFile
	for rel, text := range files {
		src = append(src, SourceFile{Rel: rel, Text: text, Lines: strings.Split(text, "\n")})
	}
	sort.Slice(src, func(i, j int) bool { return src[i].Rel < src[j].Rel })
	var out []string
	for _, e := range ExtractPythonGo(src, NewBuilder()) {
		out = append(out, e.Method+" "+e.Path)
	}
	sort.Strings(out)
	return out
}

func wantNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestFastAPIAndFlask(t *testing.T) {
	got := pyGoNames(t, map[string]string{
		"app/main.py": `from fastapi import FastAPI
from app.routers import items
app = FastAPI()
app.include_router(items.router, prefix="/api")
@app.get("/health")
def health(): ...
`,
		"app/routers/items.py": `from fastapi import APIRouter
router = APIRouter(prefix="/items")
@router.get("/{item_id}")
def read(item_id: int): ...
@router.api_route("/", methods=["POST", "PUT"])
def write(): ...
@cache.get("key")
def not_a_route(): ...
`,
		"web/views.py": `"""Example: @bp.route("/docs-example")"""
from flask import Blueprint
bp = Blueprint("users", __name__, url_prefix="/users")
@bp.route("/<int:id>", methods=('GET', 'DELETE',))
def user(id): ...
# @bp.route("/commented-out")
`,
	})
	wantNames(t, got,
		"GET /health",
		"GET /api/items/:item_id", "POST /api/items", "PUT /api/items",
		"GET /users/:id", "DELETE /users/:id")
}

func TestDjangoIncludesAndRouters(t *testing.T) {
	got := pyGoNames(t, map[string]string{
		"site/urls.py": `"""url(r'^$', views.home) is an example, not a route"""
from django.urls import include, path
urlpatterns = [
    path("api/", include("shop.urls")),
    path("admin/", admin.site.urls),
]
`,
		"shop/urls.py": `from rest_framework.routers import DefaultRouter
router = DefaultRouter()
router.register(r'orders', OrderViewSet)
urlpatterns = [
    path("", include(router.urls)),
    path("carts/<uuid:cart_id>/", views.cart),
    re_path(r'^tags/?$', views.tags),
    re_path(r'^weird/(\d+)/$', views.weird),
]
`,
	})
	wantNames(t, got,
		"ANY /admin/", "ANY /api/orders/", "ANY /api/orders/:pk/",
		"ANY /api/carts/:cart_id/", "ANY /api/tags/")
}

func TestGoRouters(t *testing.T) {
	got := pyGoNames(t, map[string]string{
		"main.go": `package main
import "github.com/gin-gonic/gin"
func main() {
	r := gin.Default()
	v1 := r.Group("/api")
	users.Register(v1.Group("/users"))
	v1.GET("/ping", ping)
	resp, _ := http.Get("/not-a-route")
}`,
		"users/routes.go": `package users
func Register(router *gin.RouterGroup) {
	router.POST("/login", login)
	router.GET("/:id", get)
}`,
		"chi/server.go": `package chi
import "github.com/go-chi/chi/v5"
func routes(r chi.Router) {
	r.Route("/orders", func(r chi.Router) {
		r.Get("/", list)
		r.Get("/{orderID}", get)
	})
	client.Get("/x")
}`,
		"std/main.go": `package main
func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /reports/{id}", report)
	mux.HandleFunc("/healthz", health)
	r.HandleFunc("/items/{id}", item).Methods("DELETE")
}`,
		"users/routes_test.go": `package users
func TestX(t *testing.T) { r.GET("/from-a-test", nil) }`,
	})
	wantNames(t, got,
		"GET /api/ping", "POST /api/users/login", "GET /api/users/:id",
		"GET /orders", "GET /orders/:orderID",
		"GET /reports/:id", "ANY /healthz", "DELETE /items/:id")
}
