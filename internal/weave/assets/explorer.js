
const $=s=>document.querySelector(s);
const KINDS={page:'Screen',endpoint:'API endpoint',module:'Module',table:'Table',test:'Test',req:'Requirement',change:'Change',task:'Task',call:'Broken call',file:'File'};
// The four lines of the map, in the order a request travels.
const LANES=[
 {key:'screens',k:'page',lane:'screen',name:'Screens',one:'screen',verb:'call'},
 {key:'endpoints',k:'endpoint',lane:'api',name:'Endpoints',one:'endpoint',verb:'handled in'},
 {key:'modules',k:'module',lane:'module',name:'Modules',one:'module',verb:'use'},
 {key:'tables',k:'table',lane:'data',name:'Tables',one:'table',verb:''}
];
const LANE_OF={page:'screen',endpoint:'api',module:'module',table:'data'};
const PLACES=[['today','Today'],['map','Map'],['changes','Changes'],['checks','Checks']];
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
const SHOW=6;
let S=D.projects[0],ix=new Map(),out=new Map(),inn=new Map();
let place='today',lane='screens',sel=null,q='',untestedOnly=false,focusSel=false,fromList=false,listKey='';

const esc=s=>String(s==null?'':s).replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const kindOf=id=>{const n=ix.get(id);return n?n.k:'file'};
const nameOf=id=>{const n=ix.get(id);return n?n.n:id};
const matches=s=>!q||String(s==null?'':s).toLowerCase().includes(q);
const enc=id=>'#node/'+encodeURIComponent(id);
const narrow=()=>typeof matchMedia==='function'&&matchMedia('(max-width:900px)').matches;
const cnt=(n,w)=>n+' '+w+(n===1?'':'s');
const laneBy=k=>LANES.filter(l=>l.k===k||l.key===k||l.lane===k)[0];
const ICON={
 check:'<svg class="ico" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 8.5 6.5 12 13 4.5"/></svg>',
 cross:'<svg class="ico" viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8"/></svg>',
 chev:'<svg class="ico sep" viewBox="0 0 16 16" aria-hidden="true"><path d="M6 3.5 10.5 8 6 12.5"/></svg>',
 back:'<svg class="ico" viewBox="0 0 16 16" aria-hidden="true"><path d="M10 3.5 5.5 8l4.5 4.5"/></svg>',
 next:'<svg class="ico" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 8h9.5M9 4.5 12.5 8 9 11.5"/></svg>'
};

function repoName(){return S.name||String(S.root).replace(/\\/g,'/').split('/').filter(Boolean).pop()||S.root}
function load(i){
  S=D.projects[i];sel=null;q='';untestedOnly=false;$('#q').value='';
  ix=new Map();out=new Map();inn=new Map();
  for(const n of S.nodes)ix.set(n.i,n);
  for(const e of S.edges){
    const a=e[0],b=e[1],k=e[2];
    if(!out.has(a))out.set(a,[]);
    if(!inn.has(b))inn.set(b,[]);
    out.get(a).push([b,k]);inn.get(b).push([a,k]);
  }
  const proj=$('#proj');
  if(D.projects.length>1){
    proj.hidden=false;
    proj.innerHTML=D.projects.map((p,n)=>'<option value="'+n+'"'+(n===i?' selected':'')+'>'+esc(p.name)+'</option>').join('');
  } else proj.hidden=true;
  document.title='weave — '+repoName();
  place='today';readHash();render();
}
// Old links keep working: #now, #summary, #screens, #api, #data, #problems.
const LEGACY={now:'today',summary:'today',screens:'map/screens',api:'map/endpoints',data:'map/tables',problems:'checks'};
function readHash(){
  const h=location.hash||'';
  if(h.indexOf('#node/')===0){
    let id='';
    try{id=decodeURIComponent(h.slice(6))}catch(e){id=''}
    if(ix.has(id)){sel=id;placeFor(id);return}
  }
  let v=h.charAt(0)==='#'?h.slice(1):'';
  if(v==='search'){sel=null;return}
  if(LEGACY[v])v=LEGACY[v];
  const parts=v.split('/');
  if(parts[0]==='map'){place='map';const l=laneBy(parts[1]||'');if(l)lane=l.key;sel=null;return}
  for(const p of PLACES)if(p[0]===parts[0]){place=p[0];sel=null;return}
  if(!v){place='today';sel=null}
}
function placeFor(id){
  if(q)return;
  const k=kindOf(id),l=laneBy(k);
  if(l){place='map';lane=l.key;return}
  if(k==='change'||k==='task'||k==='req'){place='changes';return}
  if(k==='call'){place='checks';return}
}

/* ---------------------------------------------------------------- chrome */
function render(){
  const solo=place==='today'&&!q;
  const main=$('#main');
  main.classList.toggle('solo',solo);
  main.classList.toggle('has-sel',!!sel&&!solo);
  renderTabs();
  renderCrumbs();
  if(solo)$('#list').innerHTML='';else renderList();
  renderDetail();
  if(!fromList&&!narrow()){const cur=$('#list [aria-current="true"]');if(cur&&cur.scrollIntoView)cur.scrollIntoView({block:'center'})}
  fromList=false;
  if(focusSel){
    focusSel=false;
    const target=narrow()?$('#detail h1'):$('#list [data-go][aria-current="true"]');
    if(target)target.focus();
  }
}
function renderTabs(){
  const broken=brokenCount(),open=S.changes.filter(c=>c.done<c.total).length;
  const badge={changes:open?'<span class="tc">'+open+'</span>':'',checks:broken?'<span class="tc bad">'+broken+'</span>':''};
  $('#tabs').innerHTML=PLACES.map(p=>{
    const href=p[0]==='map'?'#map/'+lane:'#'+p[0];
    return '<a href="'+href+'"'+(!q&&place===p[0]?' aria-current="page"':'')+'>'+p[1]+(badge[p[0]]||'')+'</a>';
  }).join('');
}
function renderCrumbs(){
  const c=[['#today',repoName()]];
  if(q)c.push(['#search','Search “'+q+'”']);
  else if(place==='today'){c.push([null,'Today'])}
  else if(place==='map'){c.push(['#map/'+lane,'Map']);c.push(['#map/'+lane,laneBy(lane).name])}
  else if(place==='changes')c.push(['#changes','Changes']);
  else if(place==='checks')c.push(['#checks','Checks']);
  if(sel&&ix.has(sel)){
    const n=ix.get(sel);
    if(n.k==='change')c.push([null,changeTitle(n.i).title]);
    else c.push([null,n.n]);
  }
  // The last link is the way back; the last crumb is where you are.
  const items=c.map((x,i)=>{
    const last=i===c.length-1;
    if(last||!x[0])return '<li><span'+(last?' aria-current="page"':'')+'>'+esc(x[1])+'</span></li>';
    return '<li'+(i===c.length-2?' class="up"':'')+'><a href="'+x[0]+'">'+(i===c.length-2?'<span class="upico">'+ICON.back+'</span>':'')+esc(x[1])+'</a>'+ICON.chev+'</li>';
  });
  const d=new Date(S.builtAt);
  const stamp=isNaN(d)?'':'Indexed '+d.toLocaleDateString('en',{day:'numeric',month:'short'})+', '+d.toLocaleTimeString('en',{hour:'2-digit',minute:'2-digit',hour12:false});
  $('#crumbs').innerHTML='<ol>'+items.join('')+'</ol>'+(stamp?'<span class="stamp">'+esc(stamp)+'</span>':'');
}

/* ---------------------------------------------------------------- facts */
function relOut(id,kind){const r=[];for(const e of out.get(id)||[])if((!kind||e[1]===kind)&&ix.has(e[0])&&r.indexOf(e[0])<0)r.push(e[0]);return r}
function relIn(id,kind){const r=[];for(const e of inn.get(id)||[])if((!kind||e[1]===kind)&&ix.has(e[0])&&r.indexOf(e[0])<0)r.push(e[0]);return r}
function isTested(id){return (inn.get(id)||[]).some(e=>e[1]==='hits'||e[1]==='covers')}
function hasE2e(id){return (inn.get(id)||[]).some(e=>e[1]==='exercises')}
function issue(key){for(const g of S.issues)if(g.key===key)return g.items;return []}
function stillCalled(){return S.delta?S.delta.removed.filter(x=>x.k==='endpoint'&&x.c):[]}
function brokenCount(){return issue('broken_calls').length+stillCalled().length}
function testedCount(kind,a,b){
  let n=0;
  for(const node of S.nodes){
    if(node.k!==kind)continue;
    if((inn.get(node.i)||[]).some(e=>e[1]===a||(b&&e[1]===b)))n++;
  }
  return n;
}
function descOf(n){
  if(!n)return '';
  let d=n.d||n.t||'';
  if(n.k==='endpoint')d=d.replace(/^[A-Z]+ \S+ [—-] /,'');
  return plain(d);
}
function plain(s){return String(s==null?'':s).replace(/\*\*/g,'').replace(/[`]/g,'').replace(/\s+/g,' ').trim()}
function splitEp(n){const i=n.indexOf(' ');return i<0?['',n]:[n.slice(0,i),n.slice(i+1)]}
function epSort(a,b){const A=splitEp(a.n),B=splitEp(b.n);return A[1].localeCompare(B[1])||A[0].localeCompare(B[0])}
function changeTitle(id){
  const slug=String(id).replace(/^change:/,'');
  const m=slug.match(/^(\d{4}-\d{2}-\d{2})-(.*)$/);
  return {title:m?m[2]:slug,date:m?m[1]:''};
}
// Task titles carry their evidence clause; a worklist wants the instruction.
function taskTitle(t){const s=plain(t);const i=s.indexOf(' Evidence:');return i<0?s:s.slice(0,i)}
function taskParts(t){const s=taskTitle(t),m=s.match(/^(T[\w.]*)\s+([\s\S]*)$/);return m?[m[1],m[2]]:['',s]}
// "Later: …", "Not done, by choice: …" and [deferred] are parked work, not the next step.
function isLater(t){return /^(T[\w.]*\s+)?(\[?(later|deferred)\b|not done\b)/i.test(taskTitle(t))}
function tasksOf(changeId){
  const r=[];
  for(const e of out.get(changeId)||[])if(e[1]==='has_task'){const t=ix.get(e[0]);if(t)r.push(t)}
  return r;
}
function nextTask(c){
  const open=tasksOf(c.c).filter(t=>!t.dn);
  const live=open.filter(t=>!isLater(t.t||t.n));
  if(live.length)return {t:live[0],deferred:false,live:live.length,later:open.length-live.length};
  const firstOpen=c.open.filter(t=>!isLater(t))[0];
  if(firstOpen)return {t:{t:firstOpen,n:''},deferred:false,live:c.total-c.done,later:0};
  return open.length?{t:open[0],deferred:true,live:0,later:open.length}:null;
}

/* ---------------------------------------------------------------- shared markup */
function ring(k,on){return '<span class="ring" data-k="'+esc(k)+'"'+(on?' data-on="1"':'')+' aria-hidden="true"></span>'}
function nm(id){
  const n=ix.get(id),k=kindOf(id);
  if(k==='endpoint'){const p=splitEp(n.n);return '<span class="ep"><span class="mt">'+esc(p[0])+'</span><span>'+esc(p[1])+'</span></span>'}
  if(k==='change')return esc(changeTitle(id).title);
  if(k==='task'&&n.t)return esc(taskTitle(n.t));
  if(k==='req'&&n.t)return '<span class="mt">'+esc(n.n)+'</span> '+esc(plain(n.t));
  return esc(nameOf(id));
}
function ref(id){
  const k=kindOf(id);
  return '<a class="ref'+(LANE_OF[k]||k==='call'||k==='file'||k==='test'?' code':'')+'" data-go="'+esc(id)+'" href="'+enc(id)+'">'+ring(k)+'<span>'+nm(id)+'</span></a>';
}
function refList(ids,limit){
  const cap=limit||SHOW;
  let h='<ul class="refs">'+ids.map((id,i)=>'<li'+(i>=cap?' class="extra"':'')+'>'+ref(id)+'</li>').join('')+'</ul>';
  if(ids.length>cap)h+=moreBtn(ids.length);
  return h;
}
function moreBtn(n){return '<button class="more" type="button" data-more aria-expanded="false" data-n="'+n+'">Show all '+n+'</button>'}
function row(id,aside,lines){
  const k=kindOf(id);
  return '<a class="row'+(LANE_OF[k]||k==='call'||k==='test'||k==='file'?' code':'')+'" data-go="'+esc(id)+'" href="'+enc(id)+'">'+ring(k)+
    '<span class="rn">'+nm(id)+'</span>'+(aside?'<span class="ra">'+aside+'</span>':'')+
    (lines||[]).filter(Boolean).map(l=>'<span class="rl">'+l+'</span>').join('')+'</a>';
}
function grp(title,meta,cls){return '<div class="grp'+(cls?' '+cls:'')+'"><span class="gt">'+esc(title)+'</span><span class="gm">'+esc(meta)+'</span></div>'}
function empty(t,p){return '<div class="empty"><p class="et">'+esc(t)+'</p><p>'+p+'</p></div>'}
function bar(done,total,label){
  const pct=Math.round(done/Math.max(1,total)*100);
  return '<span class="bar" role="img" aria-label="'+esc(label||done+' of '+total)+'"><i style="width:'+pct+'%"></i></span>';
}
// The network strip: four lines, a station per kind, each a door into the map.
function network(big){
  const c=S.counts,n={screens:c.pages,endpoints:c.endpoints,modules:c.modules,tables:c.tables};
  return '<nav class="net'+(big?' big':'')+'" aria-label="The map">'+LANES.map(l=>
    '<a class="st" data-lane="'+l.lane+'" href="#map/'+l.key+'"'+(!big&&place==='map'&&lane===l.key&&!q?' aria-current="page"':'')+'>'+
    '<span class="stn" aria-hidden="true"></span><span class="sn"><b>'+n[l.key]+'</b> '+esc(l.name.toLowerCase())+'</span>'+
    (big&&l.verb?'<span class="sv">'+esc(l.verb)+'</span>':'')+'</a>').join('')+'</nav>';
}

/* ---------------------------------------------------------------- lists */
function renderList(){
  // Keep the scroll only while the same list is on screen.
  const key=q?'q:'+q:place+'/'+lane+'/'+untestedOnly;
  const box=$('#list'),keep=key===listKey?box.scrollTop:0;
  listKey=key;
  let h='';
  if(q)h=findList();
  else if(place==='map')h=network(false)+laneList();
  else if(place==='changes')h=changesList();
  else if(place==='checks')h=checksList();
  box.innerHTML=h;
  box.scrollTop=keep;
  for(const a of box.querySelectorAll('[data-go]')){
    if(a.dataset.go===sel)a.setAttribute('aria-current','true');
    else a.removeAttribute('aria-current');
  }
}
function lhead(title,n,extra){return '<div class="lhead"><h2>'+esc(title)+' <span class="cn">'+n+'</span></h2>'+(extra||'')+'</div>'}
function nodeMatch(n){return matches(n.n)||matches(n.f)||matches(n.d)||matches(n.t)}
function laneList(){
  if(lane==='endpoints')return apiList();
  const L=laneBy(lane);
  const rows=S.nodes.filter(n=>n.k===L.k);
  if(!rows.length)return lhead(L.name,0)+empty('No '+L.name.toLowerCase()+' found',laneEmpty(L.key));
  if(lane==='screens'){
    const flow=new Map();for(const f of S.flows)flow.set(f.p,f);
    const calls=n=>{const f=flow.get(n.i);return f?f.e.length:0};
    rows.sort((a,b)=>calls(b)-calls(a)||a.n.localeCompare(b.n));
    return lhead('Screens',rows.length,'<span class="lh">most connected first</span>')+rows.map(n=>{
      const f=flow.get(n.i),e=f?f.e.length:0,t=f?f.t.length:0;
      const meta=(e?cnt(e,'endpoint'):'calls no endpoint')+(t?' · '+cnt(t,'table'):'');
      return row(n.i,hasE2e(n.i)?'<span class="ok">'+ICON.check+'e2e</span>':'',[descOf(n)?esc(descOf(n)):'',esc(meta)]);
    }).join('');
  }
  if(lane==='modules'){
    const mod=new Map();for(const m of S.modules)mod.set(m.m,m);
    rows.sort((a,b)=>a.n.localeCompare(b.n));
    return lhead('Modules',rows.length,'<span class="lh">by name</span>')+rows.map(n=>{
      const m=mod.get(n.i),e=m?m.e.length:0,t=m?m.t.length:0;
      return row(n.i,'',[esc((e?cnt(e,'endpoint'):'no endpoints')+(t?' · uses '+cnt(t,'table'):''))]);
    }).join('');
  }
  rows.sort((a,b)=>a.n.localeCompare(b.n));
  return lhead('Tables',rows.length,'<span class="lh">by name</span>')+rows.map(n=>{
    const mods=relIn(n.i,'uses').map(nameOf);
    return row(n.i,'',[n.d?esc(plain(n.d)):'',esc(mods.length?'used by '+mods.slice(0,3).join(', ')+(mods.length>3?' +'+(mods.length-3):''):'no module uses it')]);
  }).join('');
}
function laneEmpty(k){
  return {screens:'Screens are the routes weave finds in React Router or Next.js.',endpoints:'weave reads Express, Next.js route handlers, tRPC routers and OpenAPI specs.',
    modules:'A module is the backend folder that handles endpoints.',tables:'Tables come from Prisma models.'}[k]+' Run <code>weave doctor</code> to see the files it could not parse.';
}
function apiList(){
  const all=S.nodes.filter(n=>n.k==='endpoint');
  const untested=all.filter(n=>!isTested(n.i)).length;
  const filter='<button class="toggle" type="button" id="untested" aria-pressed="'+untestedOnly+'">No test only <span class="cn">'+untested+'</span></button>';
  const keep=n=>!untestedOnly||!isTested(n.i);
  const eps=all.filter(keep);
  let h=lhead('Endpoints',eps.length,filter);
  if(!all.length)return h+empty('No endpoints found',laneEmpty('endpoints'));
  if(!eps.length)return h+empty('Every endpoint has a test','Turn the filter off to see them all.');
  const owned=new Set(),groups=[];
  for(const m of S.modules){
    const bag=m.e.map(id=>ix.get(id)).filter(Boolean).filter(keep);
    for(const id of m.e)owned.add(id);
    if(bag.length)groups.push({m,eps:bag.sort(epSort)});
  }
  groups.sort((a,b)=>nameOf(a.m.m).localeCompare(nameOf(b.m.m)));
  const loose=eps.filter(n=>!owned.has(n.i)).sort(epSort);
  const apiRow=n=>row(n.i,isTested(n.i)?'<span class="ok">'+ICON.check+'tested</span>':'',[descOf(n)?esc(descOf(n)):'']);
  for(const g of groups){
    const used=g.m.t.map(nameOf);
    h+=grp(nameOf(g.m.m),cnt(g.eps.length,'endpoint')+(used.length?' · uses '+used.slice(0,2).join(', ')+(used.length>2?' +'+(used.length-2):''):''));
    h+=g.eps.map(apiRow).join('');
  }
  if(loose.length)h+=grp('No module',cnt(loose.length,'endpoint'))+loose.map(apiRow).join('');
  return h;
}
function changesList(){
  const open=S.changes.filter(c=>c.done<c.total),done=S.changes.filter(c=>c.done>=c.total);
  if(!S.changes.length)return lhead('Changes',0)+empty('No change records','A change record is a <code>tasks.md</code> with checkbox tasks; weave links each task to the files its Evidence line names.');
  let h=lhead('Changes',S.changes.length,'<span class="lh">newest first</span>');
  if(open.length)h+=grp('In progress',cnt(open.length,'change'))+open.map(changeRow).join('');
  if(done.length)h+='<details class="grpwrap"'+(open.length?'':' open')+'><summary class="grp"><span class="gt">Done</span><span class="gm">'+cnt(done.length,'change')+'</span></summary>'+done.map(changeRow).join('')+'</details>';
  return h;
}
function changeRow(c){
  const t=changeTitle(c.c),left=c.total-c.done;
  return '<a class="row chg" data-go="'+esc(c.c)+'" href="'+enc(c.c)+'">'+ring('change',c.done>=c.total)+
    '<span class="rn">'+esc(t.title)+'</span><span class="ra num">'+c.done+'/'+c.total+'</span>'+
    '<span class="rl">'+esc(t.date)+(left?' · '+left+' open':' · done')+'</span>'+bar(c.done,c.total,c.done+' of '+c.total+' tasks done')+'</a>';
}
function checksList(){
  const broken=issue('broken_calls'),gone=stillCalled();
  const nb=broken.length+gone.length;
  let h=lhead('Checks',nb+gapCount(),'');
  h+=grp('Broken',nb?cnt(nb,'item'):'nothing',nb?'bad':'');
  if(!nb)h+='<p class="calm">'+ICON.check+'<span>Nothing <code>weave check</code> would fail: every frontend call matches an endpoint.</span></p>';
  h+=broken.map(id=>row(id,'',['matches no endpoint · '+esc(where(id))])).join('');
  h+=gone.map(x=>'<div class="row gone">'+ring('endpoint')+'<span class="rn"><s>'+esc(x.n)+'</s></span><span class="rl">removed since '+esc(S.delta.ref)+', still called by '+cnt(x.c,'file')+'</span></div>').join('');
  const gaps=[['untested_endpoints','Endpoints with no direct test'],['pages_without_e2e','Screens with no end-to-end test'],['suspect_reqs','Requirements changed after their code']];
  for(const g of gaps){
    const items=issue(g[0]);
    if(!items.length)continue;
    h+=grp(g[1],String(items.length),'gap');
    h+=items.map(id=>row(id,'',[])).join('');
  }
  if(!gapCount())h+='<p class="calm">'+ICON.check+'<span>No gaps in proof: every endpoint and screen has a test.</span></p>';
  return h;
}
function where(id){const n=ix.get(id);return n&&n.f?n.f+(n.l?':'+n.l:''):''}
function gapCount(){return issue('untested_endpoints').length+issue('pages_without_e2e').length+issue('suspect_reqs').length}

/* ---------------------------------------------------------------- today */
function today(){
  const broken=issue('broken_calls'),gone=stillCalled(),open=S.changes.filter(c=>c.done<c.total);
  const nb=broken.length+gone.length;
  let h='<div class="today"><header class="hero"><h1 tabindex="-1">'+esc(repoName())+'</h1>';
  h+=nb
    ?'<p class="gate bad">'+ICON.cross+'<span><b>'+cnt(nb,'broken contract')+'.</b> <code>weave check</code> fails until '+(nb===1?'it is':'they are')+' fixed.</span></p>'
    :'<p class="gate ok">'+ICON.check+'<span><b>The gate passes.</b> Every frontend call matches an endpoint.</span></p>';
  h+='</header>'+network(true);
  h+='<div class="cols"><div class="work">';
  h+=sinceBlock();
  h+='<section class="blk"><h2>Next up</h2>'+worklist(broken,gone,open)+'</section>';
  h+='</div>'+health()+'</div>';
  if(!S.counts.pages&&!S.counts.endpoints)h+='<p class="note">No routes were found. weave reads Express, Next.js route handlers, tRPC routers and OpenAPI specs; <code>weave doctor</code> names the files it could not parse.</p>';
  return h+'</div>';
}
function sinceBlock(){
  const d=S.delta;
  let h='<section class="blk"><h2>Since the last commit</h2>';
  if(!d)return h+'<p class="line">No baseline: this repo has no git history weave could read, or the page was written with <code>--no-delta</code>.</p></section>';
  const total=d.added.length+d.removed.length+d.moved.length;
  if(!total)return h+'<p class="line">Nothing on the map changed since <code>'+esc(d.ref)+'</code>: no screen, endpoint, module, table, change or requirement was added, removed or moved.</p></section>';
  // What changed, counted by kind, then the few rows that explain it.
  const kindWord=(list,verb)=>{
    const by=new Map();
    for(const x of list){const k=KINDS[x.k]||x.k;by.set(k,(by.get(k)||0)+1)}
    return [...by].map(([k,v])=>v+' '+k.toLowerCase()+(v===1?'':'s')+' '+verb).join(', ');
  };
  h+='<p class="line">'+[kindWord(d.added,'added'),kindWord(d.moved,'moved'),kindWord(d.removed,d.removed.some(x=>x.c)?'removed or archived':'removed')].filter(Boolean).join(' · ')+' since <code>'+esc(d.ref)+'</code>.</p><ul class="delta">';
  const nameOfDelta=x=>x.k==='change'?changeTitle(x.i).title:x.n;
  const deltaRow=(tag,x,cls)=>'<li'+(cls?' class="'+cls+'"':'')+'><span class="dk">'+tag+'</span><span class="dn'+(x.k==='change'?' sans':'')+'">'+ring(x.k)+esc(nameOfDelta(x))+(x.c?' <b>still called by '+cnt(x.c,'file')+'</b>':'')+'</span></li>';
  const items=[];
  for(const x of d.removed)if(x.c)items.push(deltaRow('Removed',x,'bad'));
  for(const x of d.added)items.push(ix.has(x.i)?'<li><span class="dk">Added</span>'+ref(x.i)+'</li>':deltaRow('Added',x));
  for(const x of d.moved)items.push((ix.has(x.i)?'<li><span class="dk">Moved</span>'+ref(x.i):deltaRow('Moved',x)).replace('</li>','<span class="df">'+esc(x.f||'')+'</span></li>'));
  for(const x of d.removed)if(!x.c)items.push(deltaRow('Removed',x));
  h+=items.map((x,i)=>i<4?x:x.replace('<li class="','<li class="extra ').replace('<li>','<li class="extra">')).join('')+'</ul>';
  if(items.length>4)h+=moreBtn(items.length);
  return h+'</section>';
}
// What to do next, in the order the gate would take it.
function worklist(broken,gone,open){
  const items=[];
  for(const id of broken.slice(0,3))items.push({bad:true,href:enc(id),title:'Fix the call to '+nameOf(id),sub:'it matches no endpoint · '+where(id)});
  for(const x of gone.slice(0,3))items.push({bad:true,href:'#checks',title:'Restore '+x.n+' or update its callers',sub:'removed since '+S.delta.ref+', still called by '+cnt(x.c,'file')});
  const parked=[];
  for(const c of open){
    const nt=nextTask(c);
    const t=changeTitle(c.c);
    if(!nt)continue;
    const p=taskParts(nt.t.t||nt.t.n);
    const it={href:enc(c.c),title:t.title,task:p,sub:t.date+' · '+c.done+' of '+c.total+' done',c};
    if(nt.deferred){it.sub+=' · only deferred tasks left';it.parked=true;parked.push(it)}else items.push(it);
  }
  const gaps=issue('untested_endpoints').map(id=>({id,n:relIn(id,'calls').filter(x=>kindOf(x)==='page').length})).filter(x=>x.n).sort((a,b)=>b.n-a.n).slice(0,2);
  for(const g of gaps)items.push({href:enc(g.id),title:'Add a test for '+nameOf(g.id),sub:'called from '+cnt(g.n,'screen')+', no direct test'});
  const noe2e=issue('pages_without_e2e').length;
  if(noe2e)items.push({href:'#checks',title:cnt(noe2e,'screen')+' without an end-to-end test',sub:'see them under Checks'});
  for(const p of parked)items.push(p);
  if(!items.length)return '<p class="line">Nothing is waiting: no broken call, no open task, no gap in proof.</p>';
  const cap=7;
  let h='<ol class="next">'+items.slice(0,cap).map((it,i)=>
    '<li class="'+(it.bad?'bad':'')+(it.parked?'parked':'')+'"><a href="'+it.href+'"><span class="nn" aria-hidden="true">'+(i+1)+'</span>'+
    '<span class="nt"><b>'+esc(it.title)+'</b>'+(it.task?'<span class="nk">'+(it.task[0]?'<span class="tid">'+esc(it.task[0])+'</span>':'')+esc(it.task[1])+'</span>':'')+
    '<span class="ns">'+esc(it.sub)+'</span></span>'+(it.c?'<span class="np">'+bar(it.c.done,it.c.total,it.c.done+' of '+it.c.total+' tasks done')+'</span>':'')+'</a></li>').join('')+'</ol>';
  const more=[];
  if(open.length>0)more.push('<a href="#changes">'+cnt(open.length,'change')+' in progress'+ICON.next+'</a>');
  if(gapCount())more.push('<a href="#checks">All gaps in proof'+ICON.next+'</a>');
  if(more.length)h+='<p class="morelinks">'+more.join('')+'</p>';
  return h;
}
function health(){
  const c=S.counts,cov=S.cov,calls=cov.calls;
  const te=testedCount('endpoint','hits','covers'),tp=testedCount('page','exercises');
  const sus=issue('suspect_reqs').length;
  let h='<aside class="health" aria-label="Health"><h2>Proof</h2>';
  h+=meter('Endpoints with a direct test',te,c.endpoints,'api');
  h+=meter('Screens with an end-to-end test',tp,c.pages,'screen');
  if(sus)h+='<p class="hl">'+cnt(sus,'requirement')+' changed after the code was written. <a href="#checks">Review</a></p>';
  h+='<h2>What weave could read</h2><dl class="read">';
  h+='<div><dt>Route files</dt><dd>'+cov.routes.found+' / '+cov.routes.files+'</dd></div>';
  h+='<div><dt>Frontend calls matched</dt><dd>'+calls.resolved+' / '+calls.total+'</dd></div>';
  h+='<div><dt>Task files</dt><dd>'+cov.specs.parsed+' / '+cov.specs.files+'</dd></div></dl>';
  const notes=[];
  if(cov.gaps)notes.push(cnt(cov.gaps,'file')+' looked like a contract but produced nothing.');
  if(cov.unmapped)notes.push(cnt(cov.unmapped,'file')+' call an API weave could not map: an external service, or a pattern it misses.');
  if(notes.length)h+='<p class="hl">'+esc(notes.join(' '))+' <code>weave doctor</code> names them.</p>';
  else h+='<p class="hl">Nothing it was shown went unread.</p>';
  return h+'</aside>';
}
function meter(label,n,total,lane){
  return '<div class="meter" data-lane="'+lane+'"><div class="ml"><span>'+esc(label)+'</span><b>'+n+' / '+total+'</b></div>'+bar(n,total,n+' of '+total)+'</div>';
}

/* ---------------------------------------------------------------- detail */
function renderDetail(){
  const box=$('#detail');
  if(sel&&ix.has(sel)){box.innerHTML=nodeDetail(ix.get(sel));return}
  if(q){box.innerHTML=findIntro();return}
  if(place==='today'){box.innerHTML=today();return}
  box.innerHTML=introFor();
}
const INTRO={
  screens:'Every route a person can open. Each one calls endpoints, and through them reaches modules and tables.',
  endpoints:'Every HTTP endpoint, grouped by the module that handles it. A tick means a test calls it directly.',
  modules:'The backend modules that handle endpoints, and the tables each one uses.',
  tables:'Database tables, and the modules that read or write them.',
  changes:'A change is a unit of planned work: its tasks, the requirements they meet, and the files they name.',
  checks:'Broken is what <code>weave check</code> fails on. Gaps in proof are places no test reaches yet: worth knowing, not failures.'
};
function introFor(){
  const key=place==='map'?lane:place;
  let h='<div class="intro"><h1 tabindex="-1">'+esc(place==='map'?laneBy(lane).name:place==='changes'?'Changes':'Checks')+'</h1><p>'+INTRO[key]+'</p>';
  const top=place==='map'?topConnected(laneBy(lane).k):[];
  if(top.length)h+='<h2>Busiest</h2>'+refList(top,5);
  if(place==='changes'){
    const open=S.changes.filter(c=>c.done<c.total);
    h+='<p class="line">'+cnt(open.length,'change')+' in progress, '+(S.changes.length-open.length)+' done.</p>';
  }
  if(place==='checks'){
    const nb=brokenCount(),groups=issue('untested_endpoints').length+issue('pages_without_e2e').length+issue('suspect_reqs').length;
    h+='<dl class="read"><div><dt>Broken (the gate fails)</dt><dd'+(nb?' class="bad"':'')+'>'+nb+'</dd></div>'+
      '<div><dt>Endpoints with no direct test</dt><dd>'+issue('untested_endpoints').length+'</dd></div>'+
      '<div><dt>Screens with no end-to-end test</dt><dd>'+issue('pages_without_e2e').length+'</dd></div>'+
      '<div><dt>Requirements changed after the code</dt><dd>'+issue('suspect_reqs').length+'</dd></div></dl>'+
      '<p class="line">Red marks only what the gate fails. '+cnt(groups,'gap')+' in proof is information: no test reaches those yet.</p>';
  }
  h+='<p class="hint">Pick an item on the left to see where it sits on the line.</p>';
  return h+'</div>';
}
function topConnected(k){
  return S.nodes.filter(n=>n.k===k)
    .map(n=>({n,d:(inn.get(n.i)||[]).length+(out.get(n.i)||[]).length}))
    .sort((a,b)=>b.d-a.d).slice(0,5).map(x=>x.n.i);
}
function nodeDetail(n){
  const k=n.k;
  let h='<article class="item">';
  h+='<header class="ihead"><h1 tabindex="-1">'+ring(k,true)+'<span class="'+(LANE_OF[k]||k==='call'||k==='test'||k==='file'?'code':'')+'">'+(k==='change'?esc(changeTitle(n.i).title):nm(n.i))+'</span></h1>';
  h+='<p class="sub">'+subline(n)+'</p>';
  if(n.f)h+='<button class="path" type="button" data-path="'+esc(n.f)+'" data-label="'+esc(n.f+(n.l?':'+n.l:''))+'" title="Copy the path">'+esc(n.f)+(n.l?':'+n.l:'')+'</button>';
  const desc=k==='task'?'':descOf(n);
  if(desc)h+='<p class="desc">'+esc(desc)+'</p>';
  if(n.x==='suspect')h+='<p class="note">The requirement text changed after this code was written. Check the code again.</p>';
  h+='</header>';
  if(k==='change')h+=changeFacts(n);
  else if(LANE_OF[k]){h+=diagram(n.i);h+=nodeSections(n)}
  else h+=allRelations(n);
  return h+'</article>';
}
function subline(n){
  const k=n.k,bits=[esc(KINDS[k]||k)];
  if(k==='endpoint'){
    const m=relOut(n.i,'in_module');
    if(m.length)bits.push('handled in <a href="'+enc(m[0])+'">'+esc(nameOf(m[0]))+'</a>');
    bits.push(isTested(n.i)?'<span class="ok">'+ICON.check+'tested</span>':'no direct test');
  }
  if(k==='page')bits.push(hasE2e(n.i)?'<span class="ok">'+ICON.check+'end-to-end test</span>':'no end-to-end test');
  if(k==='change'){const t=changeTitle(n.i);if(t.date)bits.push(esc(t.date))}
  if(k==='task'){bits.push(n.dn?'done':'open');const c=relIn(n.i,'has_task')[0];if(c)bits.push('in <a href="'+enc(c)+'">'+esc(changeTitle(c).title)+'</a>')}
  return bits.join(' · ');
}
function changeFacts(n){
  const c=S.changes.filter(x=>x.c===n.i)[0];
  if(!c)return allRelations(n);
  let h='<div class="progress">'+bar(c.done,c.total,c.done+' of '+c.total+' tasks done')+'<span>'+c.done+' of '+c.total+' tasks done</span></div>';
  const tasks=tasksOf(n.i),open=tasks.filter(t=>!t.dn),done=tasks.filter(t=>t.dn);
  const live=open.filter(t=>!isLater(t.t||t.n)),later=open.filter(t=>isLater(t.t||t.n));
  const entry=t=>{const p=taskParts(t.t||t.n);return '<li>'+ring('task',!!t.dn)+'<span>'+(p[0]?'<span class="tid">'+esc(p[0])+'</span>':'')+esc(p[1])+'</span></li>'};
  if(live.length){
    const p=taskParts(live[0].t||live[0].n);
    h+='<section class="sec nexttask"><h2>Next task</h2><p class="nt1">'+(p[0]?'<span class="tid">'+esc(p[0])+'</span>':'')+esc(p[1])+'</p>'+
      '<p class="hint">Agents get the same answer, with files and the command to run, from <code>weave plan '+esc(n.i.replace(/^change:/,''))+'</code>.</p></section>';
  }
  if(live.length>1)h+='<section class="sec"><h2>Open <span class="cn">'+live.length+'</span></h2><ul class="tasks">'+live.map(entry).join('')+'</ul></section>';
  if(later.length)h+='<section class="sec"><h2>Deferred <span class="cn">'+later.length+'</span></h2><ul class="tasks parked">'+later.map(entry).join('')+'</ul></section>';
  if(done.length)h+='<details class="sec"><summary><h2>Done <span class="cn">'+done.length+'</span></h2></summary><ul class="tasks done">'+done.map(entry).join('')+'</ul></details>';
  if(c.r.length)h+='<section class="sec"><h2>Requirements <span class="cn">'+c.r.length+'</span></h2><ul class="tasks">'+c.r.map(r=>'<li><span class="tid">'+esc(r.id)+'</span><span>'+esc(plain(r.title)||'untitled')+'</span></li>').join('')+'</ul></section>';
  if(c.s)h+='<section class="sec"><h2>Reach</h2><p class="line">The files its tasks name reach '+esc(c.s.replace(/\(s\)/g,'s'))+'.</p></section>';
  return h;
}

/* ---------------------------------------------------------------- the line */
function chain(id){
  const k=kindOf(id),L={screen:[],api:[],module:[],data:[]};
  const add=(l,ids)=>{for(const x of ids)if(ix.has(x)&&L[l].indexOf(x)<0)L[l].push(x)};
  if(LANE_OF[k])add(LANE_OF[k],[id]);
  const modsOf=eps=>{const r=[];for(const e of eps)for(const m of relOut(e,'in_module'))if(r.indexOf(m)<0)r.push(m);return r};
  const epsOfMods=mods=>{const r=[];for(const m of mods)for(const e of relIn(m,'in_module'))if(r.indexOf(e)<0)r.push(e);return r};
  const tablesOf=mods=>{const r=[];for(const m of mods)for(const t of relOut(m,'uses'))if(r.indexOf(t)<0)r.push(t);return r};
  const screensOf=eps=>{const r=[];for(const e of eps)for(const p of relIn(e,'calls'))if(kindOf(p)==='page'&&r.indexOf(p)<0)r.push(p);return r};
  if(k==='page'){const eps=relOut(id,'calls');add('api',eps);const mods=modsOf(eps);add('module',mods);add('data',tablesOf(mods))}
  else if(k==='endpoint'){const mods=relOut(id,'in_module');add('module',mods);add('data',tablesOf(mods));add('screen',screensOf([id]))}
  else if(k==='module'){const eps=epsOfMods([id]);add('api',eps);add('data',tablesOf([id]));add('screen',screensOf(eps))}
  else if(k==='table'){const mods=relIn(id,'uses');add('module',mods);const eps=epsOfMods(mods);add('api',eps);add('screen',screensOf(eps))}
  return L;
}
// How each station relates to the item you are on, in words.
function stationLabel(k,l,n){
  const one={screen:'screen',api:'endpoint',module:'module',data:'table'}[l];
  const N=cnt(n,one);
  const W={
    page:{api:'Calls '+N,module:'Handled in '+N,data:'Reaches '+N},
    endpoint:{screen:'Called from '+N,module:n===1?'Handled in':'Handled in '+N,data:'Its module uses '+N},
    module:{screen:'Called from '+N,api:'Handles '+N,data:'Uses '+N},
    table:{screen:'Reached from '+N,api:'Reached by '+N,module:'Used by '+N}
  };
  return (W[k]&&W[k][l])||N;
}
function diagram(id){
  const L=chain(id),k=kindOf(id),here=LANE_OF[k];
  let h='<section class="trace" aria-label="Where it sits on the line"><ol>';
  for(const l of LANES){
    const ids=L[l.lane],me=l.lane===here;
    h+='<li class="stop" data-lane="'+l.lane+'"'+(me?' data-here="1"':'')+'><span class="track" aria-hidden="true"><span class="stn"></span></span>';
    if(me){h+='<h3>This '+esc(l.one)+'</h3><p class="here">'+nm(id)+'</p>'}
    else if(!ids.length)h+='<h3 class="none">No '+esc(l.name.toLowerCase())+'</h3>';
    else h+='<h3>'+esc(stationLabel(k,l.lane,ids.length))+'</h3>'+refList(ids);
    h+='</li>';
  }
  return h+'</ol></section>';
}
function conn(id){
  const r=[];
  for(const e of out.get(id)||[])if(ix.has(e[0]))r.push({id:e[0],k:kindOf(e[0]),edge:e[1],dir:'out'});
  for(const e of inn.get(id)||[])if(ix.has(e[0]))r.push({id:e[0],k:kindOf(e[0]),edge:e[1],dir:'in'});
  return r;
}
function nodeSections(n){
  const all=conn(n.i);
  const uniq=a=>a.filter((x,i)=>a.indexOf(x)===i);
  const tests=uniq(all.filter(x=>x.k==='test').map(x=>x.id));
  const planned=uniq(all.filter(x=>x.k==='change'||x.k==='task'||x.k==='req').map(x=>x.id));
  const other=all.filter(x=>!LANE_OF[x.k]&&x.k!=='test'&&x.k!=='change'&&x.k!=='task'&&x.k!=='req'&&x.k!=='file');
  let h='<section class="sec"><h2>Proved by'+(tests.length?' <span class="cn">'+tests.length+'</span>':'')+'</h2>'+
    (tests.length?refList(tests):'<p class="line">No test reaches this directly.</p>')+'</section>';
  if(planned.length)h+='<section class="sec"><h2>Planned in <span class="cn">'+planned.length+'</span></h2>'+refList(planned)+'</section>';
  if(other.length)h+=groupsHTML(other);
  h+='<section class="sec"><h2>Reach</h2><p class="line">'+esc(reach(n.i))+'</p></section>';
  return h;
}
function block(title,ids){return '<section class="sec"><h2>'+esc(title)+' <span class="cn">'+ids.length+'</span></h2>'+refList(ids)+'</section>'}
function allRelations(n){
  const all=conn(n.i);
  if(!all.length)return '<section class="sec"><h2>Connections</h2><p class="line">Nothing points at this yet.</p></section>';
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
function findIds(){return [...ix.keys()].filter(id=>nodeMatch(ix.get(id)))}
function findList(){
  const ids=findIds();
  if(!ids.length)return lhead('Search',0)+empty('Nothing matches “'+q+'”','Try part of a route (<code>/tasks</code>), a table (<code>users</code>), a change name, or a file path. <kbd>Esc</kbd> clears the search.');
  let h=lhead('Search',ids.length,'<span class="lh">grouped by kind</span>');
  for(const k of ORDER){
    const bag=ids.filter(id=>kindOf(id)===k);
    if(!bag.length)continue;
    h+=grp(KINDS[k],String(bag.length));
    h+=bag.slice(0,30).map(id=>row(id,'',[k==='change'?'':esc(descOf(ix.get(id)))])).join('');
    if(bag.length>30)h+='<details class="grpwrap"><summary class="moresum">Show all '+bag.length+'</summary>'+bag.slice(30).map(id=>row(id,'',[])).join('')+'</details>';
  }
  return h;
}
function findIntro(){
  const n=findIds().length;
  return '<div class="intro"><h1 tabindex="-1">Search</h1><p>'+cnt(n,'match')+' for “'+esc(q)+'”, grouped by kind on the left. Open one to see where it sits on the line. <kbd>Esc</kbd> clears the search.</p></div>';
}

/* ---------------------------------------------------------------- events */
$('#tabs').addEventListener('click',e=>{
  const a=e.target.closest('a');
  if(!a)return;
  if(q){q='';$('#q').value=''}
  if(location.hash===a.getAttribute('href')){sel=null;readHash();render()}
});
$('#crumbs').addEventListener('click',e=>{
  const a=e.target.closest('a');
  if(!a)return;
  const href=a.getAttribute('href');
  if(href==='#search'){e.preventDefault();sel=null;render();return}
  if(q){q='';$('#q').value=''}
  if(location.hash===href){sel=null;readHash();render()}
});
$('#q').addEventListener('input',e=>{q=e.target.value.trim().toLowerCase();sel=null;render()});
$('#q').addEventListener('keydown',e=>{
  if(e.key==='Enter'){const first=$('#list [data-go]');if(first){e.preventDefault();focusSel=true;first.click()}}
  if(e.key==='ArrowDown'){const first=$('#list [data-go]');if(first){e.preventDefault();first.focus()}}
});
$('#proj').addEventListener('change',e=>load(Number(e.target.value)));
$('#list').addEventListener('click',e=>{
  const t=e.target.closest('#untested');
  if(t){untestedOnly=!untestedOnly;renderList();const b=$('#untested');if(b)b.focus();return}
  if(e.target.closest('[data-go]'))fromList=true;
});
$('#main').addEventListener('click',e=>{
  const m=e.target.closest('[data-more]');
  if(m){
    const box=m.parentElement,open=m.getAttribute('aria-expanded')!=='true';
    box.classList.toggle('open',open);
    m.setAttribute('aria-expanded',String(open));
    m.textContent=open?'Show fewer':'Show all '+m.dataset.n;
    return;
  }
  const b=e.target.closest('.path');
  if(!b)return;
  const text=b.dataset.path||'',label=b.dataset.label||text;
  try{navigator.clipboard.writeText(text)}catch(err){}
  $('#live').textContent=text+' copied';
  b.textContent='Copied '+label;
  b.classList.add('copied');
  setTimeout(()=>{b.textContent=label;b.classList.remove('copied')},1400);
});
$('#list').addEventListener('keydown',e=>{
  if(e.key!=='ArrowDown'&&e.key!=='ArrowUp')return;
  const items=[...document.querySelectorAll('#list [data-go]')];
  if(!items.length)return;
  e.preventDefault();
  const at=items.indexOf(document.activeElement);
  const next=items[Math.min(items.length-1,Math.max(0,at<0?0:at+(e.key==='ArrowDown'?1:-1)))];
  focusSel=true;fromList=true;next.click();
});
// Escape walks up one level: search, then the item, then back to Today.
addEventListener('keydown',e=>{
  const tag=e.target.tagName;
  const typing=tag==='INPUT'||tag==='SELECT'||tag==='TEXTAREA';
  if(e.key==='/'&&!typing){e.preventDefault();$('#q').focus();$('#q').select();return}
  if(e.key!=='Escape')return;
  if(q){
    q='';$('#q').value='';
    if(location.hash.indexOf('#node/')===0){sel=null;location.hash=place==='map'?'#map/'+lane:'#'+place}
    else render();
  } else if(sel){
    sel=null;location.hash=place==='map'?'#map/'+lane:'#'+place;
  }
  $('#q').blur();
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
  const b=$('#theme'),l=$('#themelabel');
  if(!b)return;
  const word={auto:'Auto',light:'Light',dark:'Dark'}[theme];
  if(l)l.textContent=word;
  b.setAttribute('aria-label','Theme: '+word+'. Change theme');
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
