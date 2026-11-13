package weave

import (
	"sort"
	"strings"
	"testing"
)

func rubyPHPNames(t *testing.T, files map[string]string) []string {
	t.Helper()
	var src []SourceFile
	for rel, text := range files {
		src = append(src, SourceFile{Rel: rel, Text: text, Lines: strings.Split(text, "\n")})
	}
	sort.Slice(src, func(i, j int) bool { return src[i].Rel < src[j].Rel })
	var out []string
	for _, e := range ExtractRubyPHP(src, NewBuilder()) {
		out = append(out, e.Method+" "+e.Path+" "+e.File)
	}
	sort.Strings(out)
	return out
}

func TestRailsRoutes(t *testing.T) {
	got := rubyPHPNames(t, map[string]string{
		"api/config/routes.rb": `Rails.application.routes.draw do
  # resources :ignored
  scope :api, defaults: { format: :json } do
    devise_for :users, controllers: { sessions: :sessions },
                       path_names: { sign_in: :login }
    resource :user, only: [:show, :update]
    resources :articles, param: :slug, except: [:edit, :new, :update] do
      resource :favorite, only: [:create]
      get :feed, on: :collection
      member do
        post 'publish'
      end
    end
    namespace :admin do
      resources :reports, only: :index
    end
    match 'ping', to: 'health#ping', via: [:get, :head]
  end
end
`,
		"api/app/controllers/articles_controller.rb":      "class ArticlesController\n  def index\n  end\n  def feed\n  end\nend\n",
		"api/app/controllers/admin/reports_controller.rb": "class Admin::ReportsController\n  def index\n  end\nend\n",
		"api/app/controllers/sessions_controller.rb":      "class SessionsController\n  def create\n  end\nend\n",
	})
	wantNames(t, got,
		"POST /api/users/login api/app/controllers/sessions_controller.rb",
		"DELETE /api/users/sign_out api/app/controllers/sessions_controller.rb",
		"POST /api/users api/config/routes.rb",
		"PUT /api/users api/config/routes.rb",
		"PATCH /api/users api/config/routes.rb",
		"GET /api/user api/config/routes.rb",
		"PATCH /api/user api/config/routes.rb",
		"PUT /api/user api/config/routes.rb",
		"GET /api/articles api/app/controllers/articles_controller.rb",
		"POST /api/articles api/app/controllers/articles_controller.rb",
		"GET /api/articles/:slug api/app/controllers/articles_controller.rb",
		"DELETE /api/articles/:slug api/app/controllers/articles_controller.rb",
		"POST /api/articles/:article_slug/favorite api/config/routes.rb",
		"GET /api/articles/feed api/app/controllers/articles_controller.rb",
		"POST /api/articles/:slug/publish api/app/controllers/articles_controller.rb",
		"GET /api/admin/reports api/app/controllers/admin/reports_controller.rb",
		"GET /api/ping api/config/routes.rb",
		"HEAD /api/ping api/config/routes.rb",
	)
}

func TestLaravelRoutes(t *testing.T) {
	got := rubyPHPNames(t, map[string]string{
		"routes/api.php": `<?php
// Route::get('commented', 'X@y');
Route::group(['namespace' => 'Api'], function () {
    Route::post('users/login', 'AuthController@login');
    Route::match(['put', 'patch'], 'user', 'UserController@update');
    Route::get('profiles/{user?}', 'ProfileController@show');
    Route::resource('articles', 'ArticleController', ['except' => ['create', 'edit']]);
    Route::apiResource('articles/{article}/comments', 'CommentController')->only(['index', 'store']);
});
Route::prefix('admin')->middleware('auth')->group(function () {
    Route::get('stats', [StatsController::class, 'index']);
});
`,
		"app/Http/Controllers/Api/ArticleController.php": "<?php\nclass ArticleController {\n  public function index() {}\n  public function show($a) {}\n}\n",
	})
	wantNames(t, got,
		"POST /api/users/login routes/api.php",
		"PUT /api/user routes/api.php",
		"PATCH /api/user routes/api.php",
		"GET /api/profiles/:user routes/api.php",
		"GET /api/articles app/Http/Controllers/Api/ArticleController.php",
		"POST /api/articles app/Http/Controllers/Api/ArticleController.php",
		"GET /api/articles/:article app/Http/Controllers/Api/ArticleController.php",
		"PUT /api/articles/:article app/Http/Controllers/Api/ArticleController.php",
		"PATCH /api/articles/:article app/Http/Controllers/Api/ArticleController.php",
		"DELETE /api/articles/:article app/Http/Controllers/Api/ArticleController.php",
		"GET /api/articles/:article/comments routes/api.php",
		"POST /api/articles/:article/comments routes/api.php",
		"GET /api/admin/stats routes/api.php",
	)
}

// A blank or comment line inside a call's options does not end the call: the
// benchmark's harmless edit (a blank line under devise_for) used to drop
// path_names and report POST /api/users/login as removed.
func TestRailsCallOptionsSpanBlankLines(t *testing.T) {
	got := rubyPHPNames(t, map[string]string{
		"config/routes.rb": "Rails.application.routes.draw do\n  scope :api do\n    devise_for :users, controllers: { sessions: :sessions },\n\n      # sign in at /login\n      path_names: { sign_in: :login }\n  end\nend\n",
	})
	found := false
	for _, n := range got {
		if strings.HasPrefix(n, "POST /api/users/login ") {
			found = true
		}
	}
	if !found {
		t.Errorf("POST /api/users/login missing: %v", got)
	}
}
