import { plan, changeView, context, doctor, impact, listChanges, search } from './query.ts';
import { ask, askConfig, formatAsk } from './ask.ts';
import type { Graph } from './model.ts';

// MCP over stdio, newline-delimited JSON-RPC. No SDK, no dependencies.

interface Rpc {
  jsonrpc: '2.0';
  id?: number | string | null;
  method?: string;
  params?: any;
  result?: any;
  error?: { code: number; message: string };
}

const PROTOCOL = '2024-11-05';
const SUPPORTED = ['2024-11-05', '2025-03-26', '2025-06-18'];

const instructions =
  'weave maps a repo: UI pages -> HTTP endpoints -> modules -> database tables, plus spec change records and tests. ' +
  'If the work comes from a spec change, call plan(slug) first: it returns the next task with its files, endpoints and the command to run. ' +
  'Before editing any other file, call context(files) once: page, endpoints, tables, owning change, open tasks, requirements, ' +
  'tests to run and the command to run them, in a few hundred tokens. Call impact(files) only when you need the complete list. ' +
  'Use search() instead of grepping directories. Run the tests context() names, not the whole suite. ' +
  'This server never returns file contents; every output is capped.';

const TOOL_DEFS = [
  {
    name: 'context',
    description:
      'START HERE before editing a file. One compact pack: what the file is (page/module), the endpoints it serves, tables, the spec change that owns it, open tasks, and the tests to run. Cheaper than reading files or grepping.',
    inputSchema: {
      type: 'object',
      properties: {
        files: { type: 'array', items: { type: 'string' }, description: 'repo-relative paths' },
        budget: { type: 'number', description: 'token budget, default 350' },
      },
      required: ['files'],
    },
  },
  {
    name: 'impact',
    description:
      'Full blast radius of a change: every module, endpoint, table, page, test and requirement it can reach, plus endpoints no test covers. Use when you need the complete list, not the summary.',
    inputSchema: {
      type: 'object',
      properties: {
        files: { type: 'array', items: { type: 'string' }, description: 'repo-relative paths' },
      },
      required: ['files'],
    },
  },
  {
    name: 'search',
    description: 'Find endpoints, pages, modules, tables, requirements and tests by name. One line per hit; never file contents.',
    inputSchema: {
      type: 'object',
      properties: { query: { type: 'string' }, limit: { type: 'number' } },
      required: ['query'],
    },
  },
  {
    name: 'plan',
    description:
      'The next task of a spec change, with only what is needed to start it: its files, endpoints, tables, tests and the command to run them. Call this instead of reading tasks.md.',
    inputSchema: {
      type: 'object',
      properties: {
        slug: { type: 'string', description: 'change record slug, e.g. 2026-10-22-warehouse-orders' },
        task: { type: 'string', description: 'task id (T3); omit for the next unfinished one' },
      },
      required: ['slug'],
    },
  },
  {
    name: 'ask',
    description:
      'Ask a question about this repo in plain language. A cheap model answers from the graph in a few lines and names the facts it used. Use this instead of chaining search/context/impact yourself.',
    inputSchema: {
      type: 'object',
      properties: {
        question: { type: 'string', description: 'e.g. "what does the warehouse change touch?"' },
        files: { type: 'array', items: { type: 'string' }, description: 'optional repo-relative paths to ground the answer' },
      },
      required: ['question'],
    },
  },
  {
    name: 'changes',
    description:
      'A spec change record: its tasks, open tasks, the files its tasks touch, and the blast radius of those files. With no argument, lists all change records.',
    inputSchema: { type: 'object', properties: { slug: { type: 'string' } } },
  },
  {
    name: 'status',
    description: 'Graph health: node counts, endpoints no page calls, endpoints no test covers, pages without e2e.',
    inputSchema: { type: 'object', properties: {} },
  },
];

export function serveMcp(getGraph: (project?: string) => Graph, multi = false, manager = false): void {
  process.stdin.setEncoding('utf8');
  let buf = '';
  process.stdin.on('data', (chunk) => {
    buf += chunk;
    let nl: number;
    while ((nl = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, nl).trim();
      buf = buf.slice(nl + 1);
      if (!line) continue;
      let msg: Rpc;
      try {
        msg = JSON.parse(line);
      } catch {
        continue;
      }
      handle(msg, getGraph, multi, manager);
    }
  });
  process.stdin.on('end', () => process.exit(0));
}

async function handle(msg: Rpc, getGraph: (project?: string) => Graph, multi: boolean, manager: boolean): void {
  if (msg.id === undefined || msg.id === null) return; // notification
  const reply = (result: any) => send({ jsonrpc: '2.0', id: msg.id!, result });
  const fail = (code: number, message: string) => send({ jsonrpc: '2.0', id: msg.id!, error: { code, message } });

  try {
    let g: Graph;
    switch (msg.method) {
      case 'initialize': {
        const asked = String(msg.params?.protocolVersion ?? '');
        return reply({
          protocolVersion: SUPPORTED.includes(asked) ? asked : PROTOCOL,
          capabilities: { tools: {}, resources: {}, prompts: {} },
          serverInfo: { name: 'weave', version: '0.1.0' },
          instructions: manager
            ? 'This is a repo manager, not a repo. Call ask(question) for anything that needs reasoning over the codebase: a cheap model answers in a few lines and names the facts it used. Call plan(slug) for the next task and status() for health. No tool returns file contents — if you need code, read the file yourself with your own tools.'
            : multi
              ? `${instructions} This server serves several projects: pass \`project\` (a name from \`weave projects\`) to pick one; omit it for this repo.`
              : instructions,
        });
      }
      case 'tools/list':
        return reply({ tools: toolDefs(multi, manager) });
      // Clients probe these three; empty lists cost nothing and stop noisy errors.
      case 'resources/list':
      case 'resources/templates/list':
        return reply({ resources: [], resourceTemplates: [] });
      case 'prompts/list':
        return reply({ prompts: [] });
      case 'logging/setLevel':
        return reply({});
      case 'ping':
        return reply({});
      case 'tools/call': {
        const started = performance.now();
        try {
        const name = msg.params?.name;
        const a = msg.params?.arguments ?? {};
        g = getGraph(a.project ? String(a.project) : undefined);
        let text: string;
        switch (name) {
          case 'context': {
            const files = asFiles(a.files ?? a.file);
            if (!files.length) return fail(-32602, 'context needs `files`: an array of repo-relative paths');
            text = context(g, files, Number(a.budget ?? 350));
            break;
          }
          case 'impact': {
            const files = asFiles(a.files ?? a.file);
            if (!files.length) return fail(-32602, 'impact needs `files`: an array of repo-relative paths');
            text = impact(g, files);
            break;
          }
          case 'search':
            if (!a.query) return fail(-32602, 'search needs `query`');
            text = search(g, String(a.query), Number(a.limit ?? 20));
            break;
          case 'changes':
            text = a.slug
              ? changeView(g, String(a.slug))
              : listChanges(g);
            break;
          case 'plan':
            if (!a.slug) return fail(-32602, 'plan needs `slug`; call changes() for the list');
            text = plan(g, String(a.slug), a.task ? String(a.task) : undefined);
            break;
          case 'ask':
            if (!a.question) return fail(-32602, 'ask needs `question`');
            text = formatAsk(await ask(g, String(a.question), asFiles(a.files)));
            break;
          case 'status':
            text = doctor(g) + '\n' + runtimeLine(g);
            break;
          default:
            return fail(-32602, `unknown tool ${name}; available: ${TOOL_DEFS.map((t) => t.name).join(', ')}`);
        }
        return reply({ content: [{ type: 'text', text }] });
        } finally {
          runtime.calls++;
          runtime.ms.push(performance.now() - started);
          if (runtime.ms.length > 200) runtime.ms.shift();
        }
      }
      default:
        return fail(-32601, `method not found: ${msg.method}`);
    }
  } catch (e) {
    // An exception must not kill the server; the client sees a tool error and continues.
    send({
      jsonrpc: '2.0',
      id: msg.id!,
      result: { content: [{ type: 'text', text: `weave error: ${(e as Error).message}` }], isError: true },
    });
  }
}

/** Per-process counters: what the agent actually costs, not what we hoped. */
const runtime = { start: Date.now(), calls: 0, ms: [] as number[] };

function runtimeLine(g: Graph): string {
  const up = Math.round((Date.now() - runtime.start) / 1000);
  if (!runtime.calls) return `runtime      up ${up}s · no tool calls yet`;
  const sorted = [...runtime.ms].sort((a, b) => a - b);
  const avg = runtime.ms.reduce((a, b) => a + b, 0) / runtime.ms.length;
  const p95 = sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * 0.95))];
  const lastIndex = g.perf ? ` · last index ${g.perf.totalMs}ms` : '';
  return `runtime      up ${up}s · ${runtime.calls} call(s) · avg ${avg.toFixed(1)}ms · p95 ${p95.toFixed(1)}ms${lastIndex}`;
}

function asFiles(v: unknown): string[] {  if (Array.isArray(v)) return v.map(String).filter(Boolean);
  if (typeof v === 'string' && v) return [v];
  return [];
}

/** With --all every tool takes an optional `project`; with --manager only the three
 *  "talk to it" verbs are exposed, so an expensive agent cannot do the grunt work. */
function toolDefs(multi: boolean, manager = false): unknown[] {
  const base = manager ? TOOL_DEFS.filter((t) => ['ask', 'plan', 'status'].includes(t.name)) : TOOL_DEFS;
  if (!multi) return base;
  return base.map((t) => ({
    ...t,
    inputSchema: {
      ...t.inputSchema,
      properties: {
        ...t.inputSchema.properties,
        project: { type: 'string', description: 'project name from `weave projects`; omit for this repo' },
      },
    },
  }));
}

function send(msg: Rpc): void {
  try {
    process.stdout.write(JSON.stringify(msg) + '\n');
  } catch {
    // client went away
  }
}
