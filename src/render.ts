import type { Project } from './explore.ts';
import { explorePayload, changeSummary } from './explore.ts';

// One self-contained page. No framework, no build, no network: the whole explorer
// is markup, a style block and 250 lines of DOM code.

export function renderExplore(projects: { name: string; graph: import('./model.ts').Graph }[]): string {
  const payload = build(projects);
  const data = JSON.stringify({ projects: payload }).replace(/</g, '\\u003c');
  const title = projects.length === 1 ? projects[0].name : `${projects.length} projects`;
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>weave — ${esc(title)}</title>
<style>${CSS}</style></head>
<body>
<header class="bar">
  <span class="brand">weave</span>
  <select id="proj" aria-label="Project"></select>
  <nav id="tabs" class="seg" aria-label="View"></nav>
  <input id="q" class="q" placeholder="Filter — press /" aria-label="Filter" autocomplete="off" spellcheck="false">
</header>
<main>
  <section id="list" class="list" aria-label="Results"><div class="empty"><b>Loading</b></div></section>
  <section id="detail" class="detail"></section>
</main>
<footer class="foot"><span class="lg"></span><span id="foot"></span></footer>
<script>const D=${data};${JS}</script>
</body></html>`;
}

function build(projects: { name: string; graph: import('./model.ts').Graph }[]): Project[] {
  return projects.map(({ name, graph }) => {
    const p: Project = { name, ...explorePayload(graph) };
    for (const c of p.changes) c.s = changeSummary(graph, c.c.replace(/^change:/, ''));
    return p;
  });
}

export function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

const CSS = `
:root{
  /* six type steps, six space steps, nothing in between */
  --f1:19px;--f2:16px;--f3:15px;--f4:13px;--f5:12px;--f6:10.5px;
  --s1:4px;--s2:8px;--s3:12px;--s4:16px;--s5:24px;--s6:40px;
  --bg:#fcfcfb;--panel:#fff;--fg:#15151a;--fg2:#4f4f4a;--fg3:#77776f;
  --line:#e4e4dd;--line2:#f0f0ea;
  --acc:#1a6740;--acc-bg:#e8f3ed;
  --p:#1a6740;--e:#1a5a94;--m:#8a5220;--t:#5c3d84;--g:#146b67;--r:#7d5f0d;--c:#3a3a38;--k:#7b7b74;--f:#71716a;
  --h:52px;
}
@media (prefers-color-scheme:dark){:root{
  --bg:#0f0f10;--panel:#1a1a19;--fg:#eeeee8;--fg2:#b6b6ae;--fg3:#8b8b84;
  --line:#2c2c29;--line2:#232322;
  --acc:#7cc79f;--acc-bg:#15241c;
  --p:#7cc79f;--e:#8fbae5;--m:#d5a677;--t:#c0a4e0;--g:#7ccbc6;--r:#dcc078;--c:#c4c4be;--k:#94948d;--f:#a0a099;
}}
*{box-sizing:border-box}
html,body{height:100%}
body{margin:0;background:var(--bg);color:var(--fg);font:var(--f4)/1.55 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;-webkit-font-smoothing:antialiased}
button{font:inherit;color:inherit}
.bar{position:sticky;top:0;z-index:10;display:flex;align-items:center;gap:var(--s4);height:var(--h);padding:0 var(--s4);background:var(--bg);border-bottom:1px solid var(--line)}
.brand{font:650 var(--f2)/1 ui-sans-serif;letter-spacing:-.015em}
#proj{border:1px solid var(--line);background:var(--panel);color:var(--fg2);border-radius:var(--s2);padding:var(--s1) var(--s2);font:var(--f5) ui-monospace,monospace;max-width:180px}
#proj[hidden]{display:none}
.seg{display:flex;gap:2px;background:var(--line2);border-radius:var(--s2);padding:2px;overflow-x:auto;scrollbar-width:none}
.seg::-webkit-scrollbar{display:none}
.seg button{border:0;background:transparent;color:var(--fg2);padding:var(--s1) var(--s3);border-radius:6px;cursor:pointer;font-size:var(--f5);white-space:nowrap}
.seg button:hover{color:var(--fg)}
.seg button[aria-selected=true]{background:var(--panel);color:var(--fg);box-shadow:0 1px 2px #00000014;font-weight:600}
.seg button .n{font-size:var(--f6);opacity:.7;margin-left:var(--s1)}
.q{margin-left:auto;width:200px;padding:6px var(--s3);border:1px solid var(--line);border-radius:var(--s2);background:var(--panel);color:var(--fg);font:inherit;font-size:var(--f5)}
.q:focus{outline:2px solid var(--acc);outline-offset:-1px}
main{display:grid;grid-template-columns:minmax(340px,460px) 1fr;height:calc(100% - var(--h) - 34px)}
main.solo{grid-template-columns:1fr}
main.solo .list{display:none}
main.solo .detail{max-width:960px}
@media (max-width:1100px){main{grid-template-columns:minmax(300px,380px) 1fr}}
@media (max-width:880px){main{grid-template-columns:1fr;height:auto}.q{width:120px}}
@media (max-width:720px){.brand{display:none}.bar{gap:var(--s2);padding:0 var(--s3)}}
.list{overflow:auto;border-right:1px solid var(--line);padding:var(--s2) var(--s2) var(--s6)}
@media (max-width:880px){.list{max-height:44vh;border-right:0;border-bottom:1px solid var(--line)}}
.detail{overflow:auto}

/* a flow is a card: subject, what it is, what it touches */
.flow{padding:var(--s3);border-radius:var(--s2);border-bottom:1px solid var(--line2);cursor:default}
.flow:last-child{border-bottom:0}
.flow:hover{background:var(--panel)}
.flow[aria-selected=true]{background:var(--acc-bg)}
.flow[aria-selected=true] .fsub .name{color:var(--acc)}
.fsub{display:flex;align-items:baseline;gap:var(--s2);width:100%}
.fsub .name{font:600 var(--f3)/1.3 ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--fg);border:0;background:none;padding:0;cursor:pointer;text-align:left}
.fsub .name:hover{color:var(--acc)}
.fdesc{margin:var(--s1) 0 var(--s3);font-size:var(--f5);line-height:1.5;color:var(--fg2)}
.rel{display:grid;grid-template-columns:100px 1fr;gap:var(--s1) var(--s3);padding:3px 0;align-items:start}
.rel .k{font:600 var(--f6)/1.6 ui-sans-serif;text-transform:uppercase;letter-spacing:.07em;color:var(--fg3)}
.rel .v{display:flex;flex-wrap:wrap;gap:var(--s1)}
.rel.in .k{color:var(--fg3);opacity:.75}

.item{display:block;width:100%;text-align:left;border:0;background:transparent;border-radius:var(--s2);padding:var(--s3);cursor:pointer;position:relative;border-bottom:1px solid var(--line2)}
.item:hover{background:var(--panel)}
.item[aria-selected=true]{background:var(--acc-bg)}
.item[aria-selected=true]::before{content:"";position:absolute;left:0;top:var(--s3);bottom:var(--s3);width:3px;border-radius:0 2px 2px 0;background:var(--acc)}
.item .n{font:600 var(--f4)/1.35 ui-monospace,SFMono-Regular,Menlo,monospace;word-break:break-all}
.item .s{font-size:var(--f5);line-height:1.5;color:var(--fg2);margin-top:var(--s1);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
.item .m{font:var(--f6) ui-monospace,monospace;color:var(--fg3);margin-top:var(--s2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.item .row{display:flex;justify-content:space-between;align-items:baseline;gap:var(--s2)}

.chip{font:var(--f5)/1 ui-monospace,SFMono-Regular,Menlo,monospace;padding:5px 8px;border-radius:6px;background:var(--panel);border:1px solid var(--line);color:var(--fg2);cursor:pointer;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.chip:hover{border-color:var(--acc);color:var(--acc)}
.chip:focus-visible{outline:2px solid var(--acc);outline-offset:1px}
.chip.p{color:var(--p);border-color:color-mix(in srgb,var(--p) 30%,var(--line))}
.chip.e{color:var(--e);border-color:color-mix(in srgb,var(--e) 30%,var(--line))}
.chip.m{color:var(--m);border-color:color-mix(in srgb,var(--m) 30%,var(--line))}
.chip.t{color:var(--t);border-color:color-mix(in srgb,var(--t) 30%,var(--line))}
.chip.g{color:var(--g);border-color:color-mix(in srgb,var(--g) 30%,var(--line))}
.chip.r{color:var(--r);border-color:color-mix(in srgb,var(--r) 30%,var(--line))}
.chip.c{color:var(--c)}.chip.k{color:var(--k)}.chip.f{color:var(--f)}
.chip.x{color:#b4341f;border-color:color-mix(in srgb,#b4341f 40%,var(--line))}
.chip[data-bad]{border-color:#b4341f;color:#b4341f}
.tag{font:600 var(--f6)/1 ui-sans-serif;text-transform:uppercase;letter-spacing:.08em;padding:3px 6px;border-radius:5px;background:var(--line2);color:var(--fg3);white-space:nowrap}
.tag.bad{background:color-mix(in srgb,#b4341f 12%,transparent);color:#a83220}
.arrow{color:var(--fg3);font-size:var(--f6)}

/* detail */
.dhead{position:sticky;top:0;z-index:5;background:var(--bg);padding:var(--s5) var(--s5) var(--s3);border-bottom:1px solid var(--line)}
.dhead h1{margin:0;font:600 var(--f1)/1.3 ui-monospace,SFMono-Regular,Menlo,monospace;word-break:break-all}
.dmeta{margin-top:var(--s2);display:flex;gap:var(--s3);align-items:baseline;flex-wrap:wrap;font-size:var(--f5);color:var(--fg3)}
.dmeta .path{font-family:ui-monospace,monospace;word-break:break-all;color:var(--fg2)}
.body{padding:0 var(--s5) var(--s6)}
.lead{font-size:var(--f3);line-height:1.62;margin:var(--s4) 0 var(--s5);max-width:70ch}
.lead.muted{font-size:var(--f4);font-style:italic;color:var(--fg3)}
.blast{font-size:var(--f5);color:var(--fg3);margin:0 0 var(--s5)}
.note{margin:0 0 var(--s4);padding:var(--s3) var(--s4);border-radius:var(--s2);font-size:var(--f4);line-height:1.5;background:color-mix(in srgb,#b4341f 7%,transparent);border-left:3px solid #b4341f}
.sect{font:600 var(--f6)/1 ui-sans-serif;text-transform:uppercase;letter-spacing:.08em;color:var(--fg3);margin:var(--s5) 0 var(--s2)}

/* overview */
.ov{padding:var(--s5) var(--s5) var(--s6);max-width:920px}
.ov h1{margin:0 0 var(--s1);font:650 var(--f1)/1.25 ui-sans-serif;letter-spacing:-.01em;word-break:break-all}
.ov .sub{color:var(--fg3);font-size:var(--f5);margin-bottom:var(--s5)}
.stats{display:flex;flex-wrap:wrap;gap:var(--s5);margin-bottom:var(--s5)}
.stat b{display:block;font:650 var(--f2)/1.1 ui-sans-serif;font-variant-numeric:tabular-nums}
.stat span{font-size:var(--f6);text-transform:uppercase;letter-spacing:.07em;color:var(--fg3)}
.cov{display:flex;gap:var(--s3);align-items:flex-start;padding:var(--s3) var(--s4);border-radius:var(--s2);background:var(--line2);margin-bottom:var(--s5);font-size:var(--f4)}
.cov .dot{width:8px;height:8px;border-radius:50%;margin-top:6px;flex:0 0 auto}
.cov.ok .dot{background:var(--acc)}
.cov.bad .dot{background:#b4341f}
.cov .line{color:var(--fg2);font-family:ui-monospace,monospace;font-size:var(--f5);margin-top:2px}
.callout{margin:0 0 var(--s3);padding:var(--s3) var(--s4);border-radius:var(--s2);border:1px solid var(--line)}
.callout.err{border-color:color-mix(in srgb,#b4341f 42%,transparent);background:color-mix(in srgb,#b4341f 6%,transparent)}
.callout.warn{border-color:color-mix(in srgb,var(--r) 42%,transparent);background:color-mix(in srgb,var(--r) 7%,transparent)}
.callout h4{margin:0 0 var(--s2);font-size:var(--f4)}
.callout .v{display:flex;flex-wrap:wrap;gap:var(--s1)}
.prog{height:4px;background:var(--line);border-radius:999px;overflow:hidden;margin-top:var(--s2);width:200px}
.prog i{display:block;height:100%;background:var(--acc)}
.hint{color:var(--fg3);font-size:var(--f5);margin-top:var(--s5)}
.kbd{border:1px solid var(--line);border-bottom-width:2px;border-radius:4px;padding:0 5px;font:var(--f6) ui-monospace,monospace;color:var(--fg2)}
.empty{padding:var(--s6) var(--s5);max-width:52ch}
.empty b{display:block;font-size:var(--f3);margin-bottom:var(--s1)}
.empty p{margin:0;color:var(--fg2);font-size:var(--f4);line-height:1.6}
.foot{position:fixed;bottom:0;left:0;right:0;height:34px;display:flex;align-items:center;gap:var(--s4);padding:0 var(--s4);background:var(--bg);border-top:1px solid var(--line);font-size:var(--f6);color:var(--fg3)}
.lg i{display:inline-block;width:7px;height:7px;border-radius:2px;margin:0 var(--s1) 0 var(--s3);vertical-align:0}
.lg i:first-child{margin-left:0}
.lg i.p{background:var(--p)}.lg i.e{background:var(--e)}.lg i.m{background:var(--m)}.lg i.t{background:var(--t)}
.lg i.g{background:var(--g)}.lg i.r{background:var(--r)}.lg i.c{background:var(--c)}.lg i.k{background:var(--k)}.lg i.f{background:var(--f)}
#foot{margin-left:auto;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
`;

const JS = `
const $=s=>document.querySelector(s);
const CLS={page:'p',endpoint:'e',module:'m',table:'t',test:'g',req:'r',change:'c',task:'k',call:'x',file:'f'};
const VIEWS=[['overview','Overview'],['flows','Flows'],['endpoints','Endpoints'],['pages','Pages'],['tables','Tables'],['specs','Specs'],['risk','Risk'],['issues','Issues']];
let S=D.projects[0],ix=new Map(),out=new Map(),inn=new Map();
let view='overview',q='',sel=null,visible=[],pi=0;

const esc=s=>String(s).replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const lbl=id=>{const n=ix.get(id);return n?n.n:id};
const kindOf=id=>{const n=ix.get(id);return n?n.k:'call'};
const clsOf=id=>CLS[kindOf(id)]||'k';
const match=s=>!q||String(s||'').toLowerCase().includes(q);

function chip(id){const n=ix.get(id);if(!n)return '';
  return '<button class="chip '+clsOf(id)+'" data-go="'+esc(id)+'"'+(n.x==='suspect'?' data-bad="1"':'')+'>'+esc(n.n)+'</button>'}
function tagged(name,kindLabel,bad){return '<div class="fsub"><button class="name" data-go="'+esc(name)+'">'+esc(ix.get(name)?.n||name)+'</button>'+
  (kindLabel?'<span class="tag'+(bad?' bad':'')+'">'+esc(kindLabel)+'</span>':'')+'</div>'}

function load(i){pi=i;S=D.projects[i];sel=null;q='';$('#q').value='';
  ix=new Map(S.nodes.map(n=>[n.i,n]));out=new Map();inn=new Map();
  for(const [a,b,k] of S.edges){if(!out.has(a))out.set(a,[]);if(!inn.has(b))inn.set(b,[]);out.get(a).push([b,k]);inn.get(b).push([a,k]);}
  const name=repoName();
  if(D.projects.length>1){$('#proj').hidden=false;
    $('#proj').innerHTML=D.projects.map((p,n)=>'<option value="'+n+'"'+(n===i?' selected':'')+'>'+esc(p.name)+'</option>').join('');}
  else $('#proj').hidden=true;
  $('#foot').textContent=name+' · '+S.nodes.length+' nodes · '+S.edges.length+' edges · '+new Date(S.builtAt).toLocaleString();
  tabs();list();detail();
}
function repoName(){return S.root.replace(/\\\\/g,'/').split('/').filter(Boolean).pop()||S.root}
function tabs(){
  $('#tabs').innerHTML=VIEWS.map(([k,t])=>{
    const n=k==='issues'&&S.issues.length?'<span class="n">'+S.issues.length+'</span>':'';
    return '<button data-v="'+k+'" aria-selected="'+(view===k)+'">'+t+n+'</button>'}).join('');
}

/* ---------------------------------------------------------------- overview */
function listOverview(){
  const c=S.counts;
  const cov=S.cov.gaps?'bad':'ok';
  let h='<div class="ov"><h1>'+esc(repoName())+'</h1>'+
    '<div class="sub">'+S.nodes.length+' nodes · '+S.edges.length+' edges · indexed '+new Date(S.builtAt).toLocaleString()+'</div>'+
    '<div class="stats">'+
      '<div class="stat"><b>'+c.pages+'</b><span>pages</span></div>'+
      '<div class="stat"><b>'+c.endpoints+'</b><span>endpoints</span></div>'+
      '<div class="stat"><b>'+c.tables+'</b><span>tables</span></div>'+
      '<div class="stat"><b>'+c.modules+'</b><span>modules</span></div>'+
      '<div class="stat"><b>'+c.tests+'</b><span>tests</span></div>'+
      '<div class="stat"><b>'+c.changes+'</b><span>changes</span></div>'+
    '</div>';
  h+='<div class="cov '+cov+'"><span class="dot"></span><div><b>'+(S.cov.gaps?S.cov.gaps+' gap'+(S.cov.gaps>1?'s':''):'Every contract was extracted')+'</b>'+
    '<div class="line">'+esc(S.cov.line)+'</div></div></div>';
  if(S.issues.length){
    h+='<div class="sect">Needs attention</div>';
    for(const g of S.issues)h+='<div class="callout '+(g.kind==='error'?'err':'warn')+'"><h4>'+esc(g.title)+'</h4>'+
      '<div class="v">'+g.items.slice(0,8).map(i=>chip(i)).join('')+(g.items.length>8?'<span class="arrow">+'+(g.items.length-8)+'</span>':'')+'</div></div>';
  }
  const open=S.changes.filter(c=>c.done<c.total).slice(0,3);
  if(open.length){
    h+='<div class="sect">In flight</div>';
    for(const c of open){
      const pct=Math.round(c.done/Math.max(1,c.total)*100);
      h+='<button class="item" data-go="'+esc(c.c)+'" style="border-bottom:0;padding-left:0;padding-right:0">'+
        '<div class="row"><div class="n">'+esc(c.c)+'</div><div class="m">'+c.done+'/'+c.total+'</div></div>'+
        (c.sum?'<div class="s">'+esc(c.sum)+'</div>':'')+
        '<div class="prog"><i style="width:'+pct+'%"></i></div></button>';
    }
  }
  h+='<div class="hint">Pick any item, or press <span class="kbd">/</span> to search.</div></div>';
  return h;
}

/* ---------------------------------------------------------------- flows */
function listFlows(){
  const rows=S.flows.filter(f=>match(f.p)||f.e.some(e=>match(lbl(e)))||f.t.some(t=>match(lbl(t))));
  if(!rows.length)return none('No flows match.');
  return rows.map(f=>{
    const pg=ix.get(f.p);
    return '<div class="flow">'+tagged(f.p,'page')+
      (pg&&pg.d?'<p class="fdesc">'+esc(pg.d)+'</p>':'<p class="fdesc" style="font-style:italic;opacity:.6">No description in the source.</p>')+
      '<div class="rel"><div class="k">calls</div><div class="v">'+f.e.map(e=>chip(e)).join('')+'</div></div>'+
      (f.t.length?'<div class="rel"><div class="k">reads</div><div class="v">'+f.t.map(t=>chip(t)).join('')+'</div></div>':'')+
      '</div>';
  }).join('');
}

/* ---------------------------------------------------------------- indexes */
function rowsOf(kind){
  return S.nodes.filter(n=>n.k===kind&&(match(n.n)||match(n.f)||match(n.d)))
    .sort((a,b)=>a.n.localeCompare(b.n))
    .map(n=>'<button class="item" data-go="'+esc(n.i)+'">'+
      '<div class="row"><div class="n">'+esc(n.n)+'</div>'+(testedBadge(n.i))+'</div>'+
      (n.d?'<div class="s">'+esc(n.d)+'</div>':'')+
      (n.f?'<div class="m">'+esc(n.f)+'</div>':'')+'</button>').join('');
}
function testedBadge(id){
  if(kindOf(id)!=='endpoint')return '';
  const tested=(inn.get(id)||[]).some(([a,k])=>k==='hits'||k==='covers');
  return '<span class="tag'+(tested?'':' bad')+'">'+(tested?'tested':'no test')+'</span>';
}
function listPages(){const h=rowsOf('page');return h||none('No pages match.')}
function listTables(){const h=rowsOf('table');return h||none('No tables match.')}
function listEndpoints(){const h=rowsOf('endpoint');return h||none('No endpoints match.')}

function listSpecs(){
  const rows=S.changes.filter(c=>match(c.c)||match(c.sum)||c.open.some(o=>match(o)));
  if(!rows.length)return none('No change records match.');
  return rows.map(c=>{const pct=Math.round((c.done/Math.max(1,c.total))*100);
    return '<button class="item" data-go="'+esc(c.c)+'"><div class="row"><div class="n">'+esc(c.c)+'</div>'+
      '<span class="tag">'+c.done+'/'+c.total+'</span></div>'+
      (c.sum?'<div class="s">'+esc(c.sum)+'</div>':'')+
      (c.open.length?'<div class="m">next: '+esc(c.open[0])+'</div>':'<div class="m">complete</div>')+
      '<div class="prog"><i style="width:'+pct+'%"></i></div></button>'}).join('');
}
function listRisk(){
  const rows=S.nodes.filter(n=>n.k==='endpoint'||n.k==='table'||n.k==='page'||n.k==='req')
    .map(n=>({n,d:(inn.get(n.i)||[]).length})).filter(r=>r.d>0&&(match(r.n.n)||match(r.n.f)||match(r.n.d)))
    .sort((a,b)=>b.d-a.d).slice(0,80);
  if(!rows.length)return none('Nothing depends on anything yet.');
  const max=rows[0].d;
  return rows.map(r=>'<button class="item" data-go="'+esc(r.n.i)+'"><div class="row"><div class="n">'+esc(r.n.n)+'</div>'+
    '<span class="tag">'+r.d+' dep'+(r.d===1?'':'s')+'</span></div>'+
    (r.n.d?'<div class="s">'+esc(r.n.d)+'</div>':'')+
    '<div class="prog"><i style="width:'+Math.max(6,Math.round(r.d/max*100))+'%"></i></div></button>').join('');
}
function listIssues(){
  if(!S.issues.length)return '<div class="empty"><b>No issues found</b><p>No broken call sites, no endpoint without a test, no page without e2e, no requirement whose text changed.</p></div>';
  return S.issues.map(g=>'<div class="callout '+(g.kind==='error'?'err':'warn')+'" style="margin:12px 8px"><h4>'+esc(g.title)+'</h4>'+
    '<div class="v">'+g.items.slice(0,10).map(i=>chip(i)).join('')+
    (g.items.length>10?'<span class="arrow">+'+(g.items.length-10)+'</span>':'')+'</div></div>').join('');
}
function none(msg){return '<div class="empty"><b>'+esc(msg)+'</b><p>Clear the filter with <span class="kbd">Esc</span>, or switch view.</p></div>'}

function list(){
  const solo=view==='overview';
  document.querySelector('main').classList.toggle('solo',solo);
  const box=$('#list');
  if(solo){box.innerHTML='';visible=[];return;}
  const keep=box.scrollTop;
  const html=view==='flows'?listFlows():view==='endpoints'?listEndpoints():
    view==='pages'?listPages():view==='tables'?listTables():view==='specs'?listSpecs():
    view==='risk'?listRisk():listIssues();
  box.innerHTML=html;
  box.scrollTop=keep;
  visible=[...box.querySelectorAll('[data-go]')].map(e=>e.dataset.go);
  // A master list should open with something in the detail pane, not an apology.
  if(!sel&&visible.length)sel=visible[0];
  if(sel){const b=box.querySelector('[data-go="'+CSS.escape(sel)+'"]');if(b)b.setAttribute('aria-selected','true')}
}
function neighbors(id){
  return [...(out.get(id)||[]).map(([b,k])=>[b,k,'out']),...(inn.get(id)||[]).map(([a,k])=>[a,k,'in'])];
}
const REL={calls:'calls',in_module:'module',uses:'tables read',hits:'tested by',exercises:'exercises',
  asserts:'asserts',verifies:'verified by',implements:'implemented by',satisfies:'satisfies',has_task:'tasks',
  touches:'files',calls_missing:'broken call',declared_in:'declared in',specified_in:'specified in'};
const BACK={calls:'called by',in_module:'endpoints',uses:'read by',hits:'tests',exercises:'exercised by',
  asserts:'asserted in',verifies:'verifies',implements:'implemented by',satisfies:'satisfied by',
  has_task:'belongs to',touches:'named by tasks',declared_in:'declares',calls_missing:'broken call',specified_in:'specified in'};

function changeBlock(id){
  const c=S.changes.find(x=>x.c===id);if(!c)return '';
  let h='';
  if(c.r.length)h+='<div class="sect">Requirements</div>'+c.r.map(r=>
    '<div class="rel"><div class="k">'+esc(r.id)+'</div><div class="v" style="font-size:13px;color:var(--fg2)">'+esc(r.title||'(untitled)')+'</div></div>').join('');
  const tasks=(out.get(id)||[]).filter(x=>x[1]==='has_task').map(x=>ix.get(x[0])).filter(Boolean);
  if(tasks.length)h+='<div class="sect">Tasks — '+c.done+' of '+c.total+' done</div>'+tasks.map(t=>
    '<div class="rel"><div class="k">'+(t.dn?'done':'open')+'</div><div class="v" style="font-size:13px;color:'+(t.dn?'var(--fg3)':'var(--fg)')+'">'+esc(t.t||t.n)+'</div></div>').join('');
  return h;
}
function blastLine(id){
  const nb=neighbors(id);const c={};
  for(const [o] of nb)c[kindOf(o)]=(c[kindOf(o)]||0)+1;
  const words=[];const say=(n,w)=>{if(n)words.push(n+' '+w+(n>1?'s':''))};
  say(c.endpoint,'endpoint');say(c.page,'page');say(c.table,'table');say(c.test,'test');say(c.task,'task');say(c.file,'file');say(c.req,'requirement');
  return words.length?'connected to '+words.join(', '):'nothing points at this yet';
}

function detail(){
  if(!sel||!ix.get(sel)){
    $('#detail').innerHTML=view==='overview'?listOverview():
      '<div class="empty"><b>Nothing selected</b><p>Pick an item on the left. Every item shows its description, everything connected to it, and the spec change that owns it.</p></div>';
    return;
  }
  const n=ix.get(sel);
  const outs=new Map(),ins=new Map();
  for(const [b,k] of (out.get(sel)||[])){const key=REL[k]||k;(outs.get(key)||outs.set(key,new Set()).get(key)).add(b)}
  for(const [a,k] of (inn.get(sel)||[])){const key=BACK[k]||k;(ins.get(key)||ins.set(key,new Set()).get(key)).add(a)}

  const lead=n.k==='req'?[n.t,n.d].filter(Boolean).join(' — '):(n.d||n.t||'');
  let h='<div class="dhead"><h1>'+esc(n.n)+'</h1><div class="dmeta"><span class="tag">'+n.k+'</span>'+
    (n.f?'<span class="path">'+esc(n.f)+(n.l?':'+n.l:'')+'</span>':'')+'</div></div><div class="body">';
  if(lead)h+='<p class="lead">'+esc(lead)+'</p>';
  else if(['endpoint','page','table','module'].includes(n.k))
    h+='<p class="lead muted">No description in the source. Write a comment above it and re-index.</p>';
  if(n.x==='suspect')h+='<p class="note">The requirement text changed after this code was written. Re-check it.</p>';
  if(n.k==='change')h+=changeBlock(sel);
  else h+='<p class="blast">'+esc(blastLine(sel))+'</p>';

  for(const [key,set] of outs){
    h+='<div class="rel"><div class="k">'+esc(key)+'</div><div class="v">'+[...set].slice(0,40).map(id=>chip(id)).join('')+
       (set.size>40?'<span class="arrow">+'+(set.size-40)+'</span>':'')+'</div></div>';}
  for(const [key,set] of ins){
    h+='<div class="rel in"><div class="k">'+esc(key)+'</div><div class="v">'+[...set].slice(0,40).map(id=>chip(id)).join('')+
       (set.size>40?'<span class="arrow">+'+(set.size-40)+'</span>':'')+'</div></div>';}
  h+='</div>';
  $('#detail').innerHTML=h;
  $('#detail').scrollTop=0;
}

$('#list').addEventListener('click',e=>{const b=e.target.closest('[data-go]');if(b){pick(b.dataset.go)}});
$('#detail').addEventListener('click',e=>{const b=e.target.closest('[data-go]');if(b){pick(b.dataset.go)}});
$('#tabs').addEventListener('click',e=>{const b=e.target.closest('[data-v]');if(b){view=b.dataset.v;sel=null;tabs();list();detail()}});
$('#proj').addEventListener('change',e=>load(Number(e.target.value)));
$('#q').addEventListener('input',e=>{q=e.target.value.trim().toLowerCase();list()});

function pick(id){
  // From the overview there is no master list: open the tab this node belongs to.
  if(view==='overview'){
    const k=kindOf(id);
    view=k==='endpoint'?'endpoints':k==='page'?'pages':k==='table'?'tables':k==='change'?'specs':'flows';
    tabs();
  }
  sel=id;list();detail();
}
addEventListener('keydown',e=>{
  if(e.key==='/'&&document.activeElement!==$('#q')){e.preventDefault();$('#q').focus();return}
  if(e.key==='Escape'){$('#q').blur();q='';$('#q').value='';list();return}
  if(e.key==='ArrowDown'||e.key==='ArrowUp'){
    if(!visible.length)return;
    e.preventDefault();
    const at=sel?visible.indexOf(sel):-1;
    const next=e.key==='ArrowDown'?Math.min(visible.length-1,at+1):Math.max(0,at<0?0:at-1);
    pick(visible[next]);
    const b=$('#list [data-go="'+CSS.escape(sel)+'"]');if(b)b.scrollIntoView({block:'nearest'});
  }
});
load(0);
`;
