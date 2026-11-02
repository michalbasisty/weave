import type { Graph } from './model.ts';
import { context, doctor, search } from './query.ts';

// The manager: a cheap model answers from graph facts, so an expensive agent
// talks to one endpoint instead of orchestrating six tool calls.

export interface AskConfig {
  baseUrl: string;
  model: string;
  apiKey: string;
  maxTokens: number;
  timeoutMs: number;
}

const env = (k: string) => process.env[k]?.trim() || '';

/** Any OpenAI-compatible endpoint works: OpenCode Go, DeepSeek direct, Ollama, vLLM. */
export function askConfig(): AskConfig | null {
  const apiKey = env('WEAVE_LLM_API_KEY') || env('OPENCODE_GO_API_KEY') || env('DEEPSEEK_API_KEY');
  if (!apiKey) return null;
  const baseUrl = (env('WEAVE_LLM_BASE_URL') || (env('DEEPSEEK_API_KEY') ? 'https://api.deepseek.com/v1' : 'https://opencode.ai/zen/go/v1')).replace(/\/+$/, '');
  const model = env('WEAVE_LLM_MODEL') || (env('DEEPSEEK_API_KEY') ? 'deepseek-chat' : 'deepseek-v4.1-flash');
  return {
    baseUrl,
    model,
    apiKey,
    maxTokens: Number(env('WEAVE_LLM_MAX_TOKENS') || 400),
    timeoutMs: Number(env('WEAVE_LLM_TIMEOUT_MS') || 20000),
  };
}

export function askHelp(): string {
  return [
    'no model configured for `ask` — set one of:',
    '  OPENCODE_GO_API_KEY   (default base https://opencode.ai/zen/go/v1, model deepseek-v4.1-flash)',
    '  DEEPSEEK_API_KEY      (default base https://api.deepseek.com/v1, model deepseek-chat)',
    '  WEAVE_LLM_API_KEY     (with WEAVE_LLM_BASE_URL and WEAVE_LLM_MODEL)',
    'the rest of weave works without any model; only `ask` needs one.',
  ].join('\n');
}

const SYSTEM = `You answer questions about a software repository using ONLY the facts given.
Rules:
- 5 lines maximum, no preamble, no markdown headings.
- Never invent a file, endpoint, table, requirement or test that is not in the facts.
- If the facts cannot answer, reply "not in the graph:" and name the specific thing missing.
- Prefer naming concrete identifiers (routes, tables, change ids) over prose.`;

/** Pull the smallest set of facts that could answer the question. */
export function gatherFacts(g: Graph, question: string, files?: string[]): string {
  const parts: string[] = [];
  if (files?.length) parts.push('context for the files in question:', context(g, files, 320));
  const words = question.toLowerCase().match(/[a-z0-9][a-z0-9._/-]{2,}/g) ?? [];
  const hits = new Set<string>();
  for (const w of words.slice(0, 6)) {
    for (const line of search(g, w, 4).split('\n')) {
      if (!line.startsWith('nothing matched')) hits.add(line);
    }
    if (hits.size >= 12) break;
  }
  if (hits.size) parts.push('matching nodes:', [...hits].slice(0, 12).join('\n'));
  parts.push('graph health:', doctor(g).split('\n').slice(0, 4).join('\n'));
  return parts.join('\n\n');
}

export interface AskResult {
  answer: string;
  facts: string;
  ms: number;
  model: string;
}

export async function ask(g: Graph, question: string, files?: string[]): Promise<AskResult | string> {
  const cfg = askConfig();
  if (!cfg) return askHelp();
  const facts = gatherFacts(g, question, files);
  const started = Date.now();
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), cfg.timeoutMs);
  try {
    const res = await fetch(`${cfg.baseUrl}/chat/completions`, {
      method: 'POST',
      signal: ctrl.signal,
      headers: { 'content-type': 'application/json', authorization: `Bearer ${cfg.apiKey}` },
      body: JSON.stringify({
        model: cfg.model,
        max_tokens: cfg.maxTokens,
        temperature: 0,
        messages: [
          { role: 'system', content: SYSTEM },
          { role: 'user', content: `FACTS\n${facts}\n\nQUESTION\n${question}` },
        ],
      }),
    });
    if (!res.ok) {
      const body = await res.text().catch(() => '');
      return `model error ${res.status}: ${body.slice(0, 200)}\n\nfacts that were available:\n${facts}`;
    }
    const json = (await res.json()) as { choices?: { message?: { content?: string } }[] };
    const answer = json.choices?.[0]?.message?.content?.trim();
    if (!answer) return `model returned nothing\n\nfacts:\n${facts}`;
    return { answer, facts, ms: Date.now() - started, model: cfg.model };
  } catch (e) {
    return `model call failed: ${(e as Error).message}\n\nfacts that were available:\n${facts}`;
  } finally {
    clearTimeout(timer);
  }
}

/** One compact block for the caller: the answer, then what it was based on. */
export function formatAsk(r: AskResult | string, budgetLines = 18): string {
  if (typeof r === 'string') return r;
  const facts = r.facts.split('\n').slice(0, Math.max(4, budgetLines - 6)).join('\n');
  return `${r.answer}\n\n-- from ${r.model} in ${r.ms}ms; facts used:\n${facts}`;
}
