package weave

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// The manager: a cheap model answers from graph facts, so an expensive agent
// talks to one endpoint instead of orchestrating six tool calls.

// AskConfig is one OpenAI-compatible endpoint.
type AskConfig struct {
	BaseURL   string
	Model     string
	APIKey    string
	MaxTokens int
	TimeoutMs int
}

// AskResult is either a plain message (Text) or a grounded answer.
type AskResult struct {
	Text   string
	Answer string
	Facts  string
	Model  string
	Ms     int64
}

func envTrim(k string) string { return strings.TrimSpace(os.Getenv(k)) }

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := envTrim(k); v != "" {
			return v
		}
	}
	return ""
}

// AskConfigFromEnv is askConfig(): any OpenAI-compatible endpoint works.
func AskConfigFromEnv() *AskConfig {
	apiKey := firstEnv("WEAVE_LLM_API_KEY", "OPENCODE_GO_API_KEY", "DEEPSEEK_API_KEY")
	if apiKey == "" {
		return nil
	}
	deepseek := envTrim("DEEPSEEK_API_KEY") != ""
	baseURL := envTrim("WEAVE_LLM_BASE_URL")
	if baseURL == "" {
		if deepseek {
			baseURL = "https://api.deepseek.com/v1"
		} else {
			baseURL = "https://opencode.ai/zen/go/v1"
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	model := envTrim("WEAVE_LLM_MODEL")
	if model == "" {
		if deepseek {
			model = "deepseek-chat"
		} else {
			model = "deepseek-v4.1-flash"
		}
	}
	maxTokens := 400
	if v := envTrim("WEAVE_LLM_MAX_TOKENS"); v != "" {
		fmt.Sscanf(v, "%d", &maxTokens)
	}
	timeoutMs := 20000
	if v := envTrim("WEAVE_LLM_TIMEOUT_MS"); v != "" {
		fmt.Sscanf(v, "%d", &timeoutMs)
	}
	return &AskConfig{BaseURL: baseURL, Model: model, APIKey: apiKey, MaxTokens: maxTokens, TimeoutMs: timeoutMs}
}

// AskHelp is the message the rest of weave works without a model.
func AskHelp() string {
	return strings.Join([]string{
		"no model configured for `ask` — set one of:",
		"  OPENCODE_GO_API_KEY   (default base https://opencode.ai/zen/go/v1, model deepseek-v4.1-flash)",
		"  DEEPSEEK_API_KEY      (default base https://api.deepseek.com/v1, model deepseek-chat)",
		"  WEAVE_LLM_API_KEY     (with WEAVE_LLM_BASE_URL and WEAVE_LLM_MODEL)",
		"the rest of weave works without any model; only `ask` needs one.",
	}, "\n")
}

const askSystem = `You answer questions about a software repository using ONLY the facts given.
Rules:
- 5 lines maximum, no preamble, no markdown headings.
- Never invent a file, endpoint, table, requirement or test that is not in the facts.
- If the facts cannot answer, reply "not in the graph:" and name the specific thing missing.
- Prefer naming concrete identifiers (routes, tables, change ids) over prose.`

// GatherFacts pulls the smallest set of facts that could answer the question.
func GatherFacts(g *Graph, question string, files []string) string {
	var parts []string
	if len(files) > 0 {
		parts = append(parts, "context for the files in question:", Context(g, files, 320, "names"))
	}
	words := lowerWords(question)
	hits := make([]string, 0, 12)
	seen := map[string]bool{}
	for _, w := range words {
		if len(hits) >= 12 {
			break
		}
		for _, line := range strings.Split(Search(g, w, 4), "\n") {
			if strings.HasPrefix(line, "nothing matched") || seen[line] {
				continue
			}
			seen[line] = true
			hits = append(hits, line)
		}
	}
	if len(hits) > 0 {
		parts = append(parts, "matching nodes:", strings.Join(hits[:min(12, len(hits))], "\n"))
	}
	health := strings.Split(Doctor(g), "\n")
	if len(health) > 4 {
		health = health[:4]
	}
	parts = append(parts, "graph health:", strings.Join(health, "\n"))
	return strings.Join(parts, "\n\n")
}

func lowerWords(q string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() >= 3 {
			out = append(out, b.String())
		}
		b.Reset()
	}
	for _, r := range strings.ToLower(q) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '/' || r == '-' {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Ask answers from the graph. A nil config, an HTTP error or an empty answer
// comes back as Text, exactly like the TypeScript union.
func Ask(g *Graph, question string, files []string) AskResult {
	cfg := AskConfigFromEnv()
	if cfg == nil {
		return AskResult{Text: AskHelp()}
	}
	facts := GatherFacts(g, question, files)
	body, _ := json.Marshal(map[string]any{
		"model":       cfg.Model,
		"max_tokens":  cfg.MaxTokens,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": askSystem},
			{"role": "user", "content": "FACTS\n" + facts + "\n\nQUESTION\n" + question},
		},
	})
	started := time.Now()
	req, err := http.NewRequest("POST", cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AskResult{Text: fmt.Sprintf("model call failed: %v\n\nfacts that were available:\n%s", err, facts)}
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+cfg.APIKey)
	client := &http.Client{Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return AskResult{Text: fmt.Sprintf("model call failed: %v\n\nfacts that were available:\n%s", err, facts)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		text := string(raw)
		if len(text) > 200 {
			text = text[:200]
		}
		return AskResult{Text: fmt.Sprintf("model error %d: %s\n\nfacts that were available:\n%s", resp.StatusCode, text, facts)}
	}
	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return AskResult{Text: fmt.Sprintf("model call failed: %v\n\nfacts that were available:\n%s", err, facts)}
	}
	if len(parsed.Choices) == 0 {
		return AskResult{Text: "model returned nothing\n\nfacts:\n" + facts}
	}
	answer := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if answer == "" {
		return AskResult{Text: "model returned nothing\n\nfacts:\n" + facts}
	}
	return AskResult{Answer: answer, Facts: facts, Model: cfg.Model, Ms: time.Since(started).Milliseconds()}
}

// FormatAsk is one compact block: the answer, then what it was based on.
func FormatAsk(r AskResult, budgetLines int) string {
	if budgetLines == 0 {
		budgetLines = 18
	}
	if r.Text != "" {
		return r.Text
	}
	lines := strings.Split(r.Facts, "\n")
	keep := budgetLines - 6
	if keep < 4 {
		keep = 4
	}
	if len(lines) > keep {
		lines = lines[:keep]
	}
	return fmt.Sprintf("%s\n\n-- from %s in %dms; facts used:\n%s", r.Answer, r.Model, r.Ms, strings.Join(lines, "\n"))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
