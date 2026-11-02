package weave

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func clearLLMEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"WEAVE_LLM_API_KEY", "OPENCODE_GO_API_KEY", "DEEPSEEK_API_KEY", "WEAVE_LLM_BASE_URL", "WEAVE_LLM_MODEL", "WEAVE_LLM_MAX_TOKENS", "WEAVE_LLM_TIMEOUT_MS"} {
		t.Setenv(k, "")
	}
}

func TestAskWithoutAKeyExplainsItself(t *testing.T) {
	clearLLMEnv(t)
	g := &Graph{Root: t.TempDir()}
	r := Ask(g, "what touches orders?", nil)
	if !strings.Contains(r.Text, "no model configured") || !strings.Contains(r.Text, "OPENCODE_GO_API_KEY") {
		t.Fatalf("expected the setup message, got %q", r.Text)
	}
}

func TestAskSendsFactsAndFormatsTheAnswer(t *testing.T) {
	clearLLMEnv(t)
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("authorization") != "Bearer test-key" {
			t.Errorf("authorization: %q", req.Header.Get("authorization"))
		}
		b, _ := io.ReadAll(req.Body)
		body = string(b)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"orders are handled in the orders module"}}]}`))
	}))
	defer srv.Close()
	t.Setenv("WEAVE_LLM_API_KEY", "test-key")
	t.Setenv("WEAVE_LLM_BASE_URL", srv.URL)
	t.Setenv("WEAVE_LLM_MODEL", "test-model")

	g := &Graph{Root: t.TempDir()}
	r := Ask(g, "what touches orders?", nil)
	if r.Text != "" {
		t.Fatalf("unexpected message: %s", r.Text)
	}
	if r.Answer != "orders are handled in the orders module" || r.Model != "test-model" {
		t.Fatalf("result: %+v", r)
	}
	for _, want := range []string{`"model":"test-model"`, "QUESTION", "what touches orders?", "FACTS", "graph health"} {
		if !strings.Contains(body, want) {
			t.Fatalf("request body missing %q: %s", want, body)
		}
	}
	out := FormatAsk(r, 18)
	if !strings.Contains(out, r.Answer) || !strings.Contains(out, "-- from test-model") || !strings.Contains(out, "facts used") {
		t.Fatalf("formatted: %s", out)
	}
}

func TestAskReportsAnHTTPError(t *testing.T) {
	clearLLMEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream is down"))
	}))
	defer srv.Close()
	t.Setenv("WEAVE_LLM_API_KEY", "test-key")
	t.Setenv("WEAVE_LLM_BASE_URL", srv.URL)
	r := Ask(&Graph{Root: t.TempDir()}, "anything", nil)
	if !strings.Contains(r.Text, "model error 502") || !strings.Contains(r.Text, "facts that were available") {
		t.Fatalf("error path: %q", r.Text)
	}
}
