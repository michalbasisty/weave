
const $=s=>document.querySelector(s);
const KINDS={page:'Screen',endpoint:'API endpoint',module:'Module',table:'Table',test:'Test',req:'Requirement',change:'Change',task:'Task',call:'Broken call',file:'File'};
const LANE_OF={page:'screen',endpoint:'api',module:'module',table:'data'};
const VIEW_OF={page:'screens',endpoint:'api',table:'data',module:'api',change:'changes',task:'changes',req:'changes',call:'problems'};
const VIEWS=[
 ['summary','Summary','the whole map, and how to read it'],
 ['screens','Screens','routes a person can open'],
 ['api','API','endpoints, grouped by module'],
 ['data','Data','database tables'],
 ['changes','Changes','spec changes and their tasks'],
 ['problems','Problems','broken calls and missing tests']
];
const SAY={
 calls:['Calls these endpoints','Called from these screens'],
 in_module:['Handled in module','Endpoints in this module'],
 uses:['Uses tables','Used by modules'],
 hits:['Tests these endpoints','Tested by'],
 covers:['Covers these endpoints','Covered by'],
 exercises:['Exercises these screens','Exercised by tests'],
 verifies:['Verifies requirement','Verified by tests'],
 implements:['Implements requirement','Implemented in'],
 satisfies:['Meets requirement','Satisfied by tasks'],
 has_task:['Tasks','Part of change'],
 touches:['Files named by tasks','Named by tasks'],
 calls_missing:['Calls an endpoint that does not exist','Broken call site in'],
 declared_in:['Route defined in','Declares routes'],
 specified_in:['Specified in','Specifies requirement']
};
const ORDER=['page','endpoint','module','table','change','task','test','req','call','file'];
let S=D.projects[0],ix=new Map(),out=new Map(),inn=new Map();
let view='summary',sel=null,q='',focusSel=false;

const esc=s=>String(s==null?'':s).replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const kindOf=id=>{const n=ix.get(id);return n?n.k:'file'};
const nameOf=id=>{const n=ix.get(id);return n?n.n:id};
const matches=s=>!q||String(s==null?'':s).toLowerCase().includes(q);
const enc=id=>'#node/'+encodeURIComponent(id);
const narrow=()=>typeof matchMedia==='function'&&matchMedia('(max-width:900px)').matches;
const cnt=(n,w)=>n+' '+w+(n===1?'':'s');

function repoName(){return S.root.replace(/\\/g,'/').split('/').filter(Boolean).pop()||S.root}
function load(i){
  S=D.projects[i];sel=null;q='';$('#q').value='';
  ix=new Map();out=new Map();inn=new Map();
  for(const n of S.nodes)ix.set(n.i,n);
  for(const e of S.edges){
    const a=e[0],b=e[1],k=e[2];
    if(!out.has(a))out.set(a,[]);
    if(!inn.has(b))inn.set(b,[]);
    out.get(a).push([b,k]);inn.get(b).push([a,k]);
  }
  if(D.projects.length>1){
    $('#proj').hidden=false;
    $('#proj').innerHTML=D.projects.map((p,n)=>'<option value="'+n+'"'+(n===i?' selected':'')+'>'+esc(p.name)+'</option>').join('');
  } else $('#proj').hidden=true;
  view='summary';readHash();render();
}
function readHash(){
  const h=location.hash||'';
  if(h.indexOf('#node/')===0){
    let id='';
    try{id=decodeURIComponent(h.slice(6))}catch(e){id=''}
    if(ix.has(id)){sel=id;if(!q)view=VIEW_OF[kindOf(id)]||view;return}
  }
  const v=h.charAt(0)==='#'?h.slice(1):'';
  for(const entry of VIEWS)if(entry[0]===v){view=v;sel=null;return}
}

/* ---------------------------------------------------------------- chrome */
function render(){
  const solo=view==='summary'&&q==='';
  const main=document.querySelector('main');
  main.classList.toggle('solo',solo);
  main.classList.toggle('has-sel',!!sel&&!solo);
  renderRail();
  if(solo)$('#list').innerHTML='';else renderList();
  renderDetail();
  if(focusSel){
    focusSel=false;
    const target=narrow()?$('#detail h1'):$('#list [data-go][aria-current="true"]');
    if(target)target.focus();
  }
}
function renderRail(){
  const counts={screens:S.counts.pages,api:S.counts.endpoints,data:S.counts.tables,changes:S.counts.changes,problems:problemCount()};
  $('#nav').innerHTML=VIEWS.map(v=>{
    const k=v[0],n=counts[k];
    return '<a href="#'+k+'"'+((!q&&view===k)?' aria-current="page"':'')+'>'+
      '<span class="rl">'+v[1]+(n?'<span class="rc">'+n+'</span>':'')+'</span>'+
      '<span class="rg">'+esc(v[2])+'</span></a>';
  }).join('');
}
function problemCount(){let n=0;for(const g of S.issues)n+=g.items.length;return n}

/* ---------------------------------------------------------------- lists */
function renderList(){
  const box=$('#list'),keep=box.scrollTop;
  let h='';
  if(q)h=findList();
  else if(view==='screens')h=screensList();
  else if(view==='api')h=apiList();
  else if(view==='data')h=dataList();
  else if(view==='changes')h=changesList();
  else if(view==='problems')h=problemsList();
  box.innerHTML=h;
  box.scrollTop=keep;
  for(const a of box.querySelectorAll('[data-go]')){
    if(a.dataset.go===sel)a.setAttribute('aria-current','true');
    else a.removeAttribute('aria-current');
  }
}
function lhead(title,n,hint){return '<div class="lhead"><b>'+esc(title)+'<span class="cn">'+n+'</span></b><span>'+esc(hint)+'</span></div>'}
function grp(title,meta){return '<div class="grp"><span class="gt">'+esc(title)+'</span><span class="gm">'+esc(meta)+'</span></div>'}
function row(id,name,desc,meta){
  return '<a class="row" data-go="'+esc(id)+'" href="'+enc(id)+'">'+
    '<span class="rn">'+esc(name)+'</span>'+
    (desc?'<span class="rd">'+esc(desc)+'</span>':'')+
    (meta?'<span class="rm">'+esc(meta)+'</span>':'')+'</a>';
}
function empty(t,p){return '<div class="empty"><b>'+esc(t)+'</b><p>'+esc(p)+'</p></div>'}
function isTested(id){return (inn.get(id)||[]).some(e=>e[1]==='hits'||e[1]==='covers')}
function relOut(id,kind){const r=[];for(const e of out.get(id)||[])if((!kind||e[1]===kind)&&ix.has(e[0]))r.push(e[0]);return r}
function relIn(id,kind){const r=[];for(const e of inn.get(id)||[])if((!kind||e[1]===kind)&&ix.has(e[0]))r.push(e[0]);return r}

function screensList(){
  const rows=S.nodes.filter(n=>n.k==='page'&&(matches(n.n)||matches(n.f)||matches(n.d)));
  if(!rows.length)return empty('No screen matches','Screens are the routes weave found in React Router or Next.js. Clear the filter, or run the doctor to see the files it could not parse.');
  const callsOf=n=>{const flow=S.flows.filter(f=>f.p===n.i)[0];return flow?flow.e.length:0};
  rows.sort((a,b)=>callsOf(b)-callsOf(a)||a.n.localeCompare(b.n));
  return lhead('Screens',rows.length,'most connected first')+rows.map(n=>{
    const flow=S.flows.filter(f=>f.p===n.i)[0];
    const calls=flow?flow.e.length:0,tables=flow?flow.t.length:0;
    const meta=(calls?cnt(calls,'endpoint'):'no endpoint calls found')+(tables?' · '+cnt(tables,'table'):'');
    return row(n.i,n.n,descOf(n),meta);
  }).join('');
}
function apiList(){
  const eps=S.nodes.filter(n=>n.k==='endpoint'&&(matches(n.n)||matches(n.f)||matches(n.d)));
  if(!eps.length)return empty('No endpoint matches','An endpoint is a route a screen can call. Clear the filter, or run the doctor to see the route files it could not parse.');
  const owned=new Map(),groups=[];
  for(const m of S.modules){
    const bag=m.e.map(id=>ix.get(id)).filter(Boolean).filter(n=>matches(n.n)||matches(n.f)||matches(n.d));
    for(const id of m.e)owned.set(id,1);
    if(bag.length)groups.push({m,eps:bag.sort(epSort)});
  }
  groups.sort((a,b)=>nameOf(a.m.m).localeCompare(nameOf(b.m.m)));
  const loose=eps.filter(n=>!owned.has(n.i)).sort(epSort);
  let h=lhead('API endpoints',eps.length,'grouped by the module that handles them');
  for(const g of groups){
    const used=g.m.t.map(nameOf).filter(Boolean);
    h+=grp(nameOf(g.m.m),cnt(g.eps.length,'endpoint')+(used.length?' · uses '+used.slice(0,3).join(', ')+(used.length>3?' +'+(used.length-3):''):''));
    h+=g.eps.map(apiRow).join('');
  }
  if(loose.length)h+=grp('No module',cnt(loose.length,'endpoint'))+loose.map(apiRow).join('');
  return h;
}
function splitEp(n){const i=n.indexOf(' ');return i<0?['',n]:[n.slice(0,i),n.slice(i+1)]}
function epSort(a,b){const A=splitEp(a.n),B=splitEp(b.n);return A[1].localeCompare(B[1])||A[0].localeCompare(B[0])}
function apiRow(n){
  const p=splitEp(n.n),t=isTested(n.i);
  return '<a class="row api" data-go="'+esc(n.i)+'" href="'+enc(n.i)+'">'+
    '<span class="mt">'+esc(p[0])+'</span><span class="rn">'+esc(p[1])+'</span>'+
    '<span class="tt'+(t?' ok':'')+'">'+(t?'tested':'no test')+'</span>'+
    (descOf(n)?'<span class="rm">'+esc(descOf(n))+'</span>':'')+'</a>';
}
function dataList(){
  const rows=S.nodes.filter(n=>n.k==='table'&&(matches(n.n)||matches(n.f)||matches(n.d))).sort((a,b)=>a.n.localeCompare(b.n));
  if(!rows.length)return empty('No table matches','Tables come from Prisma models. Clear the filter, or check that the schema file was indexed.');
  return lhead('Tables',rows.length,'the database, and who uses it')+rows.map(n=>{
    const mods=relIn(n.i,'uses').map(nameOf);
    return row(n.i,n.n,n.d||'',mods.length?'read or written by '+mods.slice(0,4).join(', ')+(mods.length>4?' +'+(mods.length-4):''):'no module uses it yet');
  }).join('');
}
function changesList(){
  const rows=S.changes.filter(c=>matches(c.c)||matches(c.sum)||c.open.some(matches));
  if(!rows.length)return empty('No change matches','A change record comes from a tasks.md file with an Evidence: line. Clear the filter.');
  const open=rows.filter(c=>c.done<c.total),done=rows.filter(c=>c.done>=c.total);
  let h=lhead('Changes',rows.length,'newest first');
  if(open.length)h+=grp('In progress',cnt(open.length,'change'))+open.map(changeRow).join('');
  if(done.length)h+='<details class="grpwrap"><summary class="grp"><span class="gt">Done</span><span class="gm">'+done.length+'</span></summary>'+done.map(changeRow).join('')+'</details>';
  return h;
}
function changeRow(c){
  const slug=c.c.replace(/^change:/,'');
  const m=slug.match(/^(\d{4}-\d{2}-\d{2})-(.*)$/);
  const title=m?m[2]:slug,date=m?m[1]:'';
  const pct=Math.round(c.done/Math.max(1,c.total)*100);
  return '<a class="row chg" data-go="'+esc(c.c)+'" href="'+enc(c.c)+'">'+
    '<span class="dt">'+esc(date)+'</span><span class="rn">'+esc(title)+'</span><span class="num">'+c.done+'/'+c.total+'</span>'+
    (c.sum?'<span class="rd">'+esc(plain(c.sum))+'</span>':'')+
    '<span class="bar"><i style="width:'+pct+'%"></i></span></a>';
}
function problemsList(){
  if(!S.issues.length)return empty('Nothing is broken','No call site misses an endpoint, every endpoint has a direct test, every screen has an end-to-end test, and no requirement changed after its code.');
  let h=lhead('Problems',problemCount(),'what the gate would report');
  for(const g of S.issues){
    const items=g.items,many=items.length>10;
    h+='<div class="probgrp'+(g.kind==='error'?' err':'')+'">'+grp(problemTitle(g),cnt(items.length,'item'))+
      '<div class="refs">'+items.slice(0,many?8:40).map(ref).join('')+'</div>'+
      (many?'<details class="more"><summary>show all '+items.length+'</summary><div class="refs">'+items.slice(8).map(ref).join('')+'</div></details>':'')+
      '</div>';
  }
  return h;
}
function problemTitle(g){
  const w={broken_calls:'Calls that match no endpoint',untested_endpoints:'Endpoints with no direct test',pages_without_e2e:'Screens with no end-to-end test',suspect_reqs:'Requirements changed after their code'};
  return w[g.key]||g.title;
}
function ref(id){return '<a class="ref" data-go="'+esc(id)+'" data-k="'+kindOf(id)+'" href="'+enc(id)+'">'+esc(nameOf(id))+'</a>'}

/* ---------------------------------------------------------------- summary */
function summary(){
  const c=S.counts;
  let h='<div class="summary">';
  h+='<h1>'+esc(repoName())+'</h1>';
  h+='<p class="lede">'+chainSentence(c)+'</p>';
  h+='<p class="stamp">Indexed '+esc(new Date(S.builtAt).toLocaleString())+'</p>';
  const example=examplePage();
  if(example)h+='<div class="sect">How to read the map</div>'+traceHTML(example,3);
  h+=trustLine();
  if(S.issues.length){
    h+='<div class="sect">Problems</div><ul class="plist">'+S.issues.map(g=>
      '<li><a href="#problems"><b class="'+(g.kind==='error'?'err':'')+'">'+g.items.length+'</b> '+esc(problemTitle(g).toLowerCase())+'</a></li>').join('')+'</ul>';
  }
  h+='<div class="sect">Test coverage</div><p class="line">'+testedCount('endpoint','hits','covers')+' of '+c.endpoints+' API endpoints have a direct test. '+
    testedCount('page','exercises')+' of '+c.pages+' screens have an end-to-end test.</p>';
  const open=S.changes.filter(x=>x.done<x.total).slice(0,3);
  if(open.length)h+='<div class="sect">Work in progress</div>'+open.map(changeRow).join('');
  if(!c.pages&&!c.endpoints)h+='<p class="line">No routes were found. weave reads Express, Next.js route handlers, tRPC routers and OpenAPI specs; the doctor names the files it could not parse.</p>';
  return h+'</div>';
}
function chainSentence(c){
  let s='';
  if(c.pages)s+=link('#screens',cnt(c.pages,'screen'))+' call ';
  else s+='No screens were found; weave sees ';
  s+=link('#api',cnt(c.endpoints,'API endpoint'));
  if(c.modules)s+=', handled in '+cnt(c.modules,'module');
  if(c.tables)s+=', stored in '+link('#data',cnt(c.tables,'table'));
  return s+'.';
}
function link(href,text){return '<a href="'+href+'">'+text+'</a>'}
function examplePage(){
  const rows=S.flows.slice().sort((a,b)=>(b.e.length+b.t.length*2)-(a.e.length+a.t.length*2));
  return rows.length?rows[0].p:'';
}
function testedCount(kind,a,b){
  let n=0;
  for(const node of S.nodes){
    if(node.k!==kind)continue;
    const hit=(inn.get(node.i)||[]).some(e=>e[1]===a||(b&&e[1]===b));
    if(hit)n++;
  }
  return n;
}
function trustLine(){
  const cov=S.cov,calls=cov.calls;
  const broken=Math.max(0,calls.total-calls.resolved);
  const head=broken?cnt(broken,'frontend call')+(broken===1?' matches':' match')+' no endpoint':'Every frontend call matches an endpoint';
  let span='Read '+cov.routes.found+' of '+cov.routes.files+' route files and '+cov.specs.parsed+' of '+cov.specs.files+' task files. '+
    'Matched '+calls.resolved+' of '+calls.total+' frontend calls';
  span+=broken?', and '+broken+' point at no endpoint.':' against a real endpoint.';
  if(cov.gaps)span+=' '+cnt(cov.gaps,'file')+' looked like a contract but produced nothing.';
  if(cov.unmapped)span+=' '+cnt(cov.unmapped,'file')+' call an API weave could not map (an external service, or a pattern it misses).';
  return '<div class="trust'+(broken?' bad':'')+'"><b>'+esc(head)+'</b><span>'+esc(span)+
    (broken?' <a href="#problems">See them</a>.':'')+'</span></div>';
}

/* ---------------------------------------------------------------- detail */
function renderDetail(){
  const box=$('#detail');
  if(sel){const n=ix.get(sel);box.innerHTML=n?nodeDetail(n):introFor(view);return}
  if(q){box.innerHTML=findIntro();return}
  if(view==='summary'){box.innerHTML=summary();return}
  box.innerHTML=introFor(view);
}
const INTRO={
  screens:['Screens','Every route a person can open. The second line counts the endpoints it calls and the tables it reaches, so you can judge a screen without opening it.'],
  api:['API endpoints','Grouped by the module that handles them. “No test” means no test calls this path directly; it is a gap to know about, not a failure.'],
  data:['Tables','Database tables and the modules that read or write them.'],
  changes:['Changes','A unit of planned work: its tasks, the requirements they satisfy, and the files they name.'],
  problems:['Problems','What the check gate reports: calls that match no endpoint, endpoints and screens with no test, requirements whose text changed.']
};
function introFor(v){
  const it=INTRO[v];
  if(!it)return '';
  const top=topConnected(v);
  return '<div class="intro"><h2>'+esc(it[0])+'</h2><p>'+esc(it[1])+'</p>'+
    (top.length?'<div class="sect">Most connected</div><div class="refs">'+top.map(ref).join('')+'</div>':'')+'</div>';
}
function topConnected(v){
  const kinds=v==='screens'?['page']:v==='api'?['endpoint']:v==='data'?['table']:[];
  if(!kinds.length)return [];
  return S.nodes.filter(n=>kinds.indexOf(n.k)>=0)
    .map(n=>({n,d:(inn.get(n.i)||[]).length+(out.get(n.i)||[]).length}))
    .sort((a,b)=>b.d-a.d).slice(0,5).map(x=>x.n.i);
}
function nodeDetail(n){
  const k=n.k,desc=descOf(n);
  let h='<div class="detail"><a class="back" href="#'+backView(k)+'">← Back to '+esc(backName(k))+'</a>';
  h+='<div class="kind" data-k="'+k+'">'+esc(KINDS[k]||k)+'</div>';
  h+='<h1 tabindex="-1">'+esc(n.n)+'</h1>';
  if(n.f)h+='<button class="path" type="button" data-path="'+esc(n.f)+'" data-label="'+esc(n.f+(n.l?':'+n.l:''))+'">'+esc(n.f)+(n.l?':'+n.l:'')+'</button>';
  if(desc)h+='<p class="desc">'+esc(desc)+'</p>';
  if(n.x==='suspect')h+='<p class="note">The requirement text changed after this code was written. Check the code again.</p>';
  if(k==='change')h+=changeFacts(n);
  if(LANE_OF[k]){
    h+=traceHTML(n.i);
    h+=nodeSections(n);
    h+='<div class="sect">Reach</div><p class="line">'+esc(reach(n.i))+'</p>';
  } else if(k!=='change'){
    h+=allRelations(n);
  }
  h+='<span class="sr" aria-live="polite" id="copied"></span>';
  return h+'</div>';
}
function descOf(n){
  let d=n.d||n.t||'';
  if(n.k==='endpoint')d=d.replace(/^[A-Z]+ \S+ [—-] /,'');
  return plain(d);
}
function plain(s){return String(s==null?'':s).replace(/\*\*/g,'').replace(/[\u0060]/g,'').replace(/\s+/g,' ').trim()}
function changeFacts(n){
  const c=S.changes.filter(x=>x.c===n.i)[0];
  if(!c)return '';
  let h='';
  if(c.r.length){
    h+='<div class="sect">Requirements <span class="cn">'+c.r.length+'</span></div>'+
      c.r.map(r=>'<div class="entry"><span class="id">'+esc(r.id)+'</span><span>'+esc(plain(r.title)||'untitled')+'</span></div>').join('');
  }
  const tasks=(out.get(n.i)||[]).filter(e=>e[1]==='has_task').map(e=>ix.get(e[0])).filter(Boolean);
  if(tasks.length){
    const open=tasks.filter(t=>!t.dn),done=tasks.filter(t=>t.dn);
    const entry=t=>'<div class="entry"><span class="id">'+(t.dn?'done':'open')+'</span><span>'+esc(plain(t.t||t.n))+'</span></div>';
    h+='<div class="sect">Tasks — '+c.done+' of '+c.total+' done</div>'+open.map(entry).join('');
    if(done.length)h+='<details class="more"><summary>'+done.length+' done</summary>'+done.map(entry).join('')+'</details>';
  }
  return h;
}
function backView(k){return VIEW_OF[k]||'summary'}
function backName(k){
  const v=backView(k);
  for(const entry of VIEWS)if(entry[0]===v)return entry[1];
  return 'Summary';
}

/* ---------------------------------------------------------------- trace */
function chain(id){
  const k=kindOf(id),L={screen:[],api:[],module:[],data:[]};
  const add=(lane,ids)=>{
    for(const x of ids){const n=ix.get(x);if(n&&L[lane].indexOf(x)<0)L[lane].push(x)}
  };
  if(LANE_OF[k])add(LANE_OF[k],[id]);
  const modsOf=eps=>{
    const r=[];
    for(const e of eps)for(const c of out.get(e)||[])if(c[1]==='in_module'&&ix.has(c[0])&&r.indexOf(c[0])<0)r.push(c[0]);
    return r;
  };
  const epsOfMods=mods=>{
    const r=[];
    for(const m of mods)for(const c of inn.get(m)||[])if(c[1]==='in_module'&&ix.has(c[0])&&r.indexOf(c[0])<0)r.push(c[0]);
    return r;
  };
  const tablesOf=mods=>{
    const r=[];
    for(const m of mods)for(const c of out.get(m)||[])if(c[1]==='uses'&&ix.has(c[0])&&r.indexOf(c[0])<0)r.push(c[0]);
    return r;
  };
  const screensOf=eps=>{
    const r=[];
    for(const e of eps)for(const c of inn.get(e)||[])if(c[1]==='calls'&&kindOf(c[0])==='page'&&r.indexOf(c[0])<0)r.push(c[0]);
    return r;
  };
  if(k==='page'){
    const eps=relOut(id,'calls');add('api',eps);
    const mods=modsOf(eps);add('module',mods);add('data',tablesOf(mods));
  } else if(k==='endpoint'){
    const mods=relOut(id,'in_module');add('module',mods);add('data',tablesOf(mods));add('screen',screensOf([id]));
  } else if(k==='module'){
    const eps=epsOfMods([id]);add('api',eps);add('data',tablesOf([id]));add('screen',screensOf(eps));
  } else if(k==='table'){
    const mods=relIn(id,'uses');add('module',mods);
    const eps=epsOfMods(mods);add('api',eps);add('screen',screensOf(eps));
  }
  return L;
}
function traceHTML(id,cap){
  const L=chain(id),on=LANE_OF[kindOf(id)]||'';
  const cols=[['screen','Screens'],['api','API endpoints'],['module','Module'],['data','Tables']];
  let h='<div class="trace">';
  for(const col of cols){
    const lane=col[0],ids=L[lane],limit=cap||8;
    h+='<div class="lane" data-lane="'+lane+'"'+(on===lane?' data-on="1"':'')+'>'+
      '<div class="lk">'+col[1]+(ids.length?'<span class="ln">'+ids.length+'</span>':'')+'</div>';
    if(!ids.length)h+='<span class="lv-empty">none</span>';
    else{
      h+=ids.slice(0,limit).map(ref).join('');
      if(ids.length>limit)h+='<details class="more"><summary>'+(ids.length-limit)+' more</summary>'+ids.slice(limit).map(ref).join('')+'</details>';
    }
    h+='</div>';
  }
  return h+'</div><p class="cap">A screen calls an endpoint, the endpoint is handled in a module, and the module uses tables. Two steps either way.</p>';
}
function conn(id){
  const r=[];
  for(const e of out.get(id)||[])if(ix.has(e[0]))r.push({id:e[0],k:kindOf(e[0]),edge:e[1],dir:'out'});
  for(const e of inn.get(id)||[])if(ix.has(e[0]))r.push({id:e[0],k:kindOf(e[0]),edge:e[1],dir:'in'});
  return r;
}
function nodeSections(n){
  const all=conn(n.i);
  const isLane=x=>x.k==='page'||x.k==='endpoint'||x.k==='module'||x.k==='table';
  const tests=all.filter(x=>x.k==='test');
  const planned=all.filter(x=>x.k==='change'||x.k==='task'||x.k==='req');
  const other=all.filter(x=>!isLane(x)&&x.k!=='test'&&x.k!=='change'&&x.k!=='task'&&x.k!=='req'&&x.k!=='file');
  let h='';
  if(tests.length)h+=block('Proved by',tests.map(x=>x.id));
  if(planned.length)h+=block('Planned in',planned.map(x=>x.id));
  if(other.length)h+=groupsHTML(other);
  return h;
}
function block(title,ids){
  return '<div class="sect">'+esc(title)+' <span class="cn">'+ids.length+'</span></div><div class="refs">'+ids.map(ref).join('')+'</div>';
}
function allRelations(n){
  const all=conn(n.i);
  if(!all.length)return '<div class="sect">Connections</div><p class="line">Nothing points at this yet.</p>';
  return groupsHTML(all);
}
function groupsHTML(all){
  const map=new Map();
  for(const x of all){
    const say=(SAY[x.edge]||[x.edge,x.edge])[x.dir==='out'?0:1];
    if(!map.has(say))map.set(say,[]);
    if(map.get(say).indexOf(x.id)<0)map.get(say).push(x.id);
  }
  let h='';
  for(const [say,ids] of map)h+=block(say,ids);
  return h;
}
function reach(id){
  const L=chain(id),k=kindOf(id),parts=[],tests=new Set();
  for(const x of conn(id))if(x.k==='test')tests.add(x.id);
  for(const e of L.api)for(const x of conn(e))if(x.k==='test')tests.add(x.id);
  if(k!=='page'&&L.screen.length)parts.push(cnt(L.screen.length,'screen'));
  if(k!=='endpoint'&&L.api.length)parts.push(cnt(L.api.length,'endpoint'));
  if(k!=='table'&&L.data.length)parts.push(cnt(L.data.length,'table'));
  if(tests.size)parts.push(cnt(tests.size,'test'));
  return parts.length?'A change here can reach '+parts.join(', ')+'.':'Nothing else points at this yet.';
}

/* ---------------------------------------------------------------- find */
function findIds(){
  return [...ix.keys()].filter(id=>{
    const n=ix.get(id);
    return matches(n.n)||matches(n.f)||matches(n.d)||matches(n.t);
  }).slice(0,400);
}
function findList(){
  const ids=findIds();
  if(!ids.length)return empty('Nothing matches','Try a route, a table, a change name, or part of a file path.');
  let h=lhead('Results',ids.length,'matching “'+q+'”');
  for(const k of ORDER){
    const bag=ids.filter(id=>kindOf(id)===k);
    if(!bag.length)continue;
    h+=grp(KINDS[k],cnt(bag.length,'match'));
    const rowOf=id=>row(id,nameOf(id),descOf(ix.get(id)),'');
    h+=bag.slice(0,30).map(rowOf).join('');
    if(bag.length>30)h+='<details class="more"><summary>show all '+bag.length+'</summary>'+bag.slice(30).map(rowOf).join('')+'</details>';
  }
  return h;
}
function findIntro(){
  const n=findIds().length;
  return '<div class="intro"><h2>Find</h2><p>'+cnt(n,'match')+' for “'+esc(q)+'”, grouped by kind on the left. Open one to see its trace; the search stays until you clear it with <span class="kbd">Esc</span>.</p></div>';
}

/* ---------------------------------------------------------------- events */
$('#nav').addEventListener('click',e=>{
  const a=e.target.closest('a');
  if(!a)return;
  q='';const input=$('#q');if(input)input.value='';
  if(location.hash===a.getAttribute('href')){sel=null;render()}
});
$('#q').addEventListener('input',e=>{q=e.target.value.trim().toLowerCase();sel=null;render()});
$('#proj').addEventListener('change',e=>load(Number(e.target.value)));
$('#detail').addEventListener('click',e=>{
  const b=e.target.closest('.path');
  if(!b)return;
  const text=b.dataset.path||'',label=b.dataset.label||text;
  try{navigator.clipboard.writeText(text)}catch(err){}
  const live=$('#copied');
  if(live)live.textContent=text+' copied';
  b.textContent=label+' copied';
  setTimeout(()=>{b.textContent=label},1200);
});
$('#list').addEventListener('keydown',e=>{
  if(e.key!=='ArrowDown'&&e.key!=='ArrowUp')return;
  const items=[...document.querySelectorAll('#list [data-go]')];
  if(!items.length)return;
  e.preventDefault();
  const at=items.indexOf(document.activeElement);
  const next=items[Math.min(items.length-1,Math.max(0,at<0?0:at+(e.key==='ArrowDown'?1:-1)))];
  focusSel=true;next.click();
});
addEventListener('keydown',e=>{
  const tag=e.target.tagName;
  const typing=tag==='INPUT'||tag==='SELECT'||tag==='TEXTAREA';
  if(e.key==='/'&&!typing){e.preventDefault();$('#q').focus();return}
  if(e.key==='Escape'){
    if(q){
      q='';$('#q').value='';
      if(location.hash.indexOf('#node/')===0){sel=null;location.hash='#'+view}
      else render();
    }
    else if(sel){sel=null;location.hash='#'+view;render();}
    $('#q').blur();
  }
});
addEventListener('hashchange',()=>{readHash();render()});

/* ---------------------------------------------------------------- theme */
const THEMES=['auto','light','dark'];
let theme='auto';
try{const saved=localStorage.getItem('weave:theme');if(THEMES.indexOf(saved)>=0)theme=saved}catch(err){}
function applyTheme(){
  const mq=typeof matchMedia==='function'?matchMedia('(prefers-color-scheme:dark)'):{matches:false};
  const dark=theme==='dark'||(theme==='auto'&&mq.matches);
  document.documentElement.dataset.theme=dark?'dark':'light';
  const b=$('#theme');
  if(!b)return;
  b.textContent='Theme: '+theme;
  b.setAttribute('aria-label','Theme: '+theme+'. Click to change');
}
$('#theme').addEventListener('click',()=>{
  theme=THEMES[(THEMES.indexOf(theme)+1)%THEMES.length];
  try{localStorage.setItem('weave:theme',theme)}catch(err){}
  applyTheme();
});
if(typeof matchMedia==='function'){
  const mq=matchMedia('(prefers-color-scheme:dark)');
  if(mq.addEventListener)mq.addEventListener('change',()=>{if(theme==='auto')applyTheme()});
}

applyTheme();
load(0);
