/* The hosted layer. Everything talks to /api/* on this origin; the token is injected
   by the server and exists only in this document. */
(() => {
  const token = (window.__weave && window.__weave.token) || '';
  const api = (path, body) => fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Weave-Token': token },
    body: body === undefined ? undefined : JSON.stringify(body),
  }).then(r => r.json());

  // One panel, every project: the explorer's own switcher swaps embedded data, but
  // here each project has its own server-side checks, specs and wiki, so a choice
  // navigates to that project's panel instead.
  const projects = (window.__weave && window.__weave.projects) || [];
  const old = document.getElementById('proj');
  if (old && projects.length > 1) {
    const proj = old.cloneNode(false); // drops the explorer's listener
    proj.hidden = false;
    for (const p of projects) {
      const o = document.createElement('option');
      o.value = p.href; o.textContent = p.name; o.selected = !!p.current;
      proj.append(o);
    }
    proj.addEventListener('change', () => {
      location.href = proj.value + (token ? '?t=' + encodeURIComponent(token) : '');
    });
    old.replaceWith(proj);
  }

  const el = (tag, props = {}, kids = []) => {
    const n = document.createElement(tag);
    for (const [k, v] of Object.entries(props)) {
      if (k === 'class') n.className = v;
      else if (k === 'text') n.textContent = v;
      else if (k === 'html') n.innerHTML = v;
      else if (k.startsWith('on')) n.addEventListener(k.slice(2), v);
      else n.setAttribute(k, v);
    }
    for (const k of kids) n.append(k);
    return n;
  };

  /* ---------------------------------------------------------------- header */
  const gatePill = el('button', { class: 'wgate', type: 'button', title: 'Run the gate again' }, [
    el('span', { class: 'wdot', id: 'wdot' }),
    el('span', { id: 'wgate-text', text: 'checking…' }),
  ]);
  const bar = el('div', { class: 'wbar', role: 'toolbar', 'aria-label': 'Actions' }, [gatePill]);
  const theme = document.getElementById('theme');
  if (theme && theme.parentElement) theme.parentElement.insertBefore(bar, theme);
  else document.body.append(bar);

  /* ---------------------------------------------------------------- the dock */
  // The transient tools: the gate output and one file window with its note. Specs and
  // the wiki are places in the menu instead, because they are read at full width.
  const dialog = el('dialog', { class: 'wdialog', 'aria-label': 'Panel' });
  const dTitle = el('b', { text: 'Panel' });
  const dKind = el('span', { class: 'wkind', text: '' });
  const dClose = el('button', { type: 'button', title: 'Close', 'aria-label': 'Close' }, [el('span', { text: '×' })]);
  const tabs = [['check', 'Check'], ['file', 'File']];
  const tabNames = {
    check: ['weave check', 'working tree'],
    file: ['File', ''],
  };
  const tabButtons = {};
  const panes = {};
  const tabBar = el('div', { class: 'wtabs', role: 'tablist', 'aria-label': 'Panel sections' });
  for (const [key, label] of tabs) {
    panes[key] = el('div', { class: 'wpane', role: 'tabpanel' });
    const b = el('button', { type: 'button', role: 'tab', 'aria-selected': 'false', text: label });
    b.addEventListener('click', () => showPanel(key));
    tabButtons[key] = b;
    tabBar.append(b);
  }
  const dBody = el('div', { class: 'wbody' }, tabs.map(([k]) => panes[k]));
  dialog.append(el('header', {}, [dTitle, dKind, dClose]), tabBar, dBody);
  document.body.append(dialog);

  const setPane = (key, nodes) => panes[key].replaceChildren(...nodes);

  const showPanel = (key, title, kind) => {
    if (!panes[key]) return;
    const wasOpen = dialog.open;
    for (const [k, b] of Object.entries(tabButtons)) b.setAttribute('aria-selected', k === key ? 'true' : 'false');
    for (const [k, p] of Object.entries(panes)) p.dataset.on = k === key ? '1' : '0';
    dTitle.textContent = title || tabNames[key][0];
    dTitle.title = dTitle.textContent;
    dKind.textContent = kind === undefined ? tabNames[key][1] : kind;
    if (!dialog.open) dialog.show();
    if (!wasOpen) tabButtons[key].focus();
  };

  dClose.addEventListener('click', () => dialog.close());
  // Escape closes the dock and must not reach the page's own Escape handler, which
  // clears the map selection.
  dialog.addEventListener('keydown', e => {
    if (e.key === 'Escape') { e.stopPropagation(); dialog.close(); }
  }, true);

  setPane('check', [el('p', { class: 'whint', text: 'Click the gate to run weave check.' })]);
  setPane('file', [el('p', { class: 'whint', text: 'Open a file from a file:line or a path chip.' })]);

  /* ------------------------------------------------- file:line is clickable */
  const linkify = text => {
    const frag = document.createDocumentFragment();
    const re = /([\w./@-]+\.\w{1,5}):(\d+)/g;
    let last = 0, m;
    while ((m = re.exec(text))) {
      frag.append(document.createTextNode(text.slice(last, m.index)));
      const a = el('a', { text: m[0], title: 'Open ' + m[0] });
      a.addEventListener('click', () => openFile(m[1], Number(m[2])));
      frag.append(a);
      last = m.index + m[0].length;
    }
    frag.append(document.createTextNode(text.slice(last)));
    return frag;
  };

  const openFile = (path, line) => {
    api('api/file?path=' + encodeURIComponent(path) + (line ? '&line=' + line : ''))
      .then(res => {
        if (res.error) {
          setPane('file', [el('p', { class: 'wbad', text: res.error })]);
          showPanel('file', path);
          return;
        }
        const pre = el('pre', {});
        pre.append(linkify(res.text));
        const note = el('div', { class: 'wnote' });
        const input = el('input', { type: 'text', placeholder: 'note for this file — why it is like this', 'aria-label': 'Note' });
        const save = el('button', { type: 'button', text: 'Remember' });
        save.addEventListener('click', () => {
          const text = input.value.trim();
          if (!text) return;
          save.disabled = true;
          api('api/note', { note: text, files: [path] }).then(r => {
            save.disabled = false;
            input.value = '';
            panes.file.prepend(el('p', { class: r.exit === 0 ? 'wok' : 'wbad', text: r.exit === 0 ? 'Saved. context() returns it as a note, not evidence.' : r.output }));
          });
        });
        note.append(input, save);
        setPane('file', [pre, note]);
        showPanel('file', res.path, 'lines ' + res.start + '–' + (res.start + res.text.split('\n').length - 1) + ' of ' + res.total);
      });
  };

  /* ---------------------------------------------------------------- the gate */
  let checkedAt = 0;
  const setGate = (kind, text) => {
    const dot = document.getElementById('wdot');
    if (dot) dot.className = 'wdot ' + kind;
    const t = document.getElementById('wgate-text');
    if (t) t.textContent = text;
  };
  const runCheck = show => {
    setGate('busy', 'checking…');
    const t0 = Date.now();
    api('api/check', {}).then(r => {
      checkedAt = Date.now();
      const bad = r.exit !== 0;
      setGate(bad ? 'bad' : 'ok', bad ? 'contract broken' : 'checked ' + Math.round((Date.now() - t0) / 100) / 10 + 's');
      const pre = el('pre', {});
      pre.append(linkify(r.output || '(no output)'));
      setPane('check', [
        el('p', { class: bad ? 'wbad' : 'wok', text: bad ? 'Exit ' + r.exit + ' — the contract is broken.' : 'Exit 0 — every call matches an endpoint.' }),
        pre,
      ]);
      if (show) showPanel('check');
    }).catch(() => setGate('bad', 'check failed'));
  };
  gatePill.addEventListener('click', () => runCheck(true));

  /* --------------------------------------------------- full views in the menu */
  // Specs and Wiki are places, not panes: the generated map is two columns, and these
  // want the whole content width. They live beside #list/#detail so the map's own
  // renderer keeps working untouched.
  const main = document.getElementById('main');
  const specsPage = el('div', { class: 'wfullpage wspecspage' });
  const wikiBar = el('div', { class: 'wwikibar' }, [
    el('b', { text: 'Wiki' }),
    el('span', { class: 'wkind', text: 'generated from the index' }),
    el('button', { type: 'button', class: 'wmark', text: 'Rebuild' }),
  ]);
  const wikiBody = el('div', { class: 'wwikibody' });
  const wikiPage = el('div', { class: 'wfullpage wwikipage' }, [wikiBar, wikiBody]);
  const fullHost = el('section', { id: 'wfullview' }, [specsPage, wikiPage]);
  if (main) main.append(fullHost);

  let activeFull = '';
  let specsDirty = false;
  let specsFocus = '';
  let specsHost = null;

  const fail = output => {
    setPane('check', [el('pre', { text: output })]);
    showPanel('check', 'weave spec', 'failed');
  };

  const tickButton = (change, task, after) => {
    const label = (task.done ? 'Reopen ' : 'Mark ') + task.id + (task.done ? '' : ' done');
    const box = el('button', {
      type: 'button',
      class: 'wtick' + (task.done ? ' done' : ''),
      'data-task': change + ' ' + task.id,
      'aria-pressed': task.done ? 'true' : 'false',
      'aria-label': label,
      title: label,
      text: task.done ? '✓' : '',
    });
    box.addEventListener('click', () => {
      box.disabled = true;
      api('api/task-done', { change, task: task.id, done: !task.done }).then(r => {
        if (r.exit !== 0) {
          box.disabled = false;
          if (specsHost) specsHost.prepend(el('p', { class: 'wbad', text: r.output }));
          else fail(r.output);
          return;
        }
        after();
      }).catch(() => { box.disabled = false; });
    });
    return box;
  };

  const renderSpecs = host => {
    specsHost = host;
    api('api/specs').then(r => {
      if (r.error) { host.replaceChildren(el('p', { class: 'wbad', text: r.error })); return; }
      const changes = r.changes || [];
      const caps = r.capabilities || [];
      const open = changes.reduce((n, c) => n + (c.total - c.done), 0);
      const tickRow = (change, task) => {
        const box = tickButton(change, task, () => {
          specsDirty = true;
          specsFocus = change + ' ' + task.id;
          renderSpecs(host);
        });
        return el('li', {}, [box, el('span', { class: 'wtask', text: task.id + '  ' + task.title })]);
      };
      const nodes = [
        el('h2', { class: 'wfullhead', text: 'Specs' }),
        el('p', {
          class: 'whint',
          text: changes.length + ' change' + (changes.length === 1 ? '' : 's') + ' · ' + open +
            ' open task' + (open === 1 ? '' : 's') + ' · ' + caps.length + ' capabilit' + (caps.length === 1 ? 'y' : 'ies') + '.',
        }),
      ];
      const views = r.views || [];
      let viewsSection = null;
      if (views.length) {
        const body = el('tbody', {});
        for (const v of views) {
          const conn = [];
          if (v.endpoints.length) {
            conn.push(v.endpoints.length + ' endpoint' + (v.endpoints.length === 1 ? '' : 's') + ': ' +
              v.endpoints.slice(0, 3).join(', ') + (v.endpoints.length > 3 ? ' +' + (v.endpoints.length - 3) : ''));
          } else {
            conn.push('no endpoint');
          }
          if (v.tables.length) {
            conn.push('tables: ' + v.tables.slice(0, 3).join(', ') + (v.tables.length > 3 ? ' +' + (v.tables.length - 3) : ''));
          }
          const screen = el('a', { href: '#node/' + encodeURIComponent(v.id), text: v.name });
          const spec = v.owner
            ? el('a', { href: '#specs/' + encodeURIComponent(v.owner), text: v.owner })
            : el('span', { class: 'wkind', text: 'unowned' });
          body.append(el('tr', {}, [
            el('td', { class: 'wvname' }, [screen]),
            el('td', { class: 'wvconn', text: conn.join(' · '), title: v.endpoints.concat(v.tables).join(', ') }),
            el('td', { class: 'wvspec' }, [spec]),
          ]));
        }
        viewsSection = el('section', { class: 'wspec' }, [
          el('div', { class: 'wspechead' }, [
            el('b', { text: 'Views' }),
            el('span', { class: 'wkind', text: String(views.length) }),
          ]),
          el('p', { class: 'whint', text: 'Every screen, the endpoints and tables it reaches, and the capability whose Parts table names its file.' }),
          el('div', { class: 'wtablewrap' }, [
            el('table', { class: 'wviewtable' }, [
              el('thead', {}, [el('tr', {}, [
                el('th', { text: 'Screen' }),
                el('th', { text: 'Connections' }),
                el('th', { text: 'Spec' }),
              ])]),
              body,
            ]),
          ]),
        ]);
      }
      // A repository with no specs gets one card that says what they are and how to
      // start, not a table of every screen marked unowned.
      if (!changes.length && !caps.length) {
        nodes.push(el('section', { class: 'wempty' }, [
          el('b', { text: 'No specs in this repository yet' }),
          el('p', { text: 'A change record is specs/changes/<slug>/tasks.md with checkbox tasks; weave links each task to the files it names, and agents read the next one with plan(). A capability spec is specs/capabilities/<area>/spec.md with a Parts table that names its files.' }),
          el('p', { html: 'Start one below, or run <code>weave spec new &lt;name&gt;</code>.' }),
        ]));
      } else if (!changes.length) {
        nodes.push(el('p', { class: 'whint', text: 'No change record yet. One is specs/changes/<slug>/tasks.md with checkbox tasks.' }));
      }
      for (const c of changes) {
        const ul = el('ul', { class: 'wtasks' });
        for (const t of c.tasks) ul.append(tickRow(c.slug, t));
        nodes.push(el('section', { class: 'wspec' }, [
          el('div', { class: 'wspechead' }, [
            el('b', { text: c.slug }),
            el('span', { class: 'wkind', text: c.done + '/' + c.total }),
          ]),
          ul,
        ]));
      }
      // Capability specs live in the repo as files; weave indexes their requirements
      // but has no node for the capability, so this reads the directory.
      if (caps.length) {
        const ul = el('ul', { class: 'wcaps' });
        for (const cap of caps) {
          const link = el('a', { href: '#specs/' + encodeURIComponent(cap.name), class: 'wcname', text: cap.name });
          const openBtn = el('button', { type: 'button', class: 'wopen', text: 'raw' });
          openBtn.addEventListener('click', () => openFile(cap.file, cap.line || 0));
          ul.append(el('li', {}, [
            link,
            el('span', { class: 'wkind', text: cap.parts + ' parts · ' + cap.reqs + ' reqs' }),
            openBtn,
          ]));
        }
        nodes.push(el('section', { class: 'wspec' }, [
          el('div', { class: 'wspechead' }, [
            el('b', { text: 'Capabilities' }),
            el('span', { class: 'wkind', text: String(caps.length) }),
          ]),
          ul,
        ]));
      }

      // Views read best once a capability owns screens; until then they fold away.
      if (viewsSection) {
        if (caps.length) nodes.push(viewsSection);
        else nodes.push(el('details', { class: 'wviews' }, [
          el('summary', { text: 'Views: ' + views.length + ' screen' + (views.length === 1 ? '' : 's') + ', none owned by a capability spec yet' }),
          viewsSection,
        ]));
      }

      const name = el('input', { type: 'text', placeholder: 'new change — billing-retry', 'aria-label': 'New change name', spellcheck: 'false' });
      const create = el('button', { type: 'button', text: 'Create' });
      create.addEventListener('click', () => {
        const value = name.value.trim();
        if (!value) return;
        create.disabled = true;
        api('api/spec-new', { name: value }).then(res => {
          create.disabled = false;
          if (res.exit !== 0) { host.prepend(el('p', { class: 'wbad', text: res.output })); return; }
          specsDirty = true;
          name.value = '';
          renderSpecs(host);
        }).catch(() => { create.disabled = false; });
      });
      nodes.push(el('div', { class: 'wnote' }, [name, create]));
      host.replaceChildren(...nodes);
      // A tick re-renders the list; keep focus on the box you pressed.
      if (specsFocus) {
        const want = specsFocus;
        specsFocus = '';
        for (const b of host.querySelectorAll('.wtick')) {
          if (b.dataset.task === want) { b.focus(); break; }
        }
      }
    });
  };

  const openSpecEditor = (host, name, path, text) => {
    const area = el('textarea', { class: 'wspecedit', spellcheck: 'false', 'aria-label': 'Spec Markdown' });
    area.value = text;
    const status = el('span', { class: 'wspecstatus', text: 'Markdown — Ctrl+S saves' });
    const save = el('button', { type: 'button', class: 'wmark', text: 'Save' });
    const cancel = el('button', { type: 'button', class: 'wopen', text: 'Cancel' });
    const bar = el('div', { class: 'wspecbar' }, [
      el('a', { href: '#specs', class: 'wback', text: '← All specs' }),
      el('span', { class: 'wkind', text: path }),
      status, cancel, save,
    ]);
    const saveIt = () => {
      save.disabled = true;
      api('api/spec-save', { path, text: area.value }).then(r => {
        save.disabled = false;
        if (r.error) {
          status.textContent = r.error;
          status.className = 'wspecstatus wbad';
          return;
        }
        renderSpecPage(host, name, 'Saved');
      }).catch(() => {
        save.disabled = false;
        status.textContent = 'Save failed';
        status.className = 'wspecstatus wbad';
      });
    };
    save.addEventListener('click', saveIt);
    cancel.addEventListener('click', () => renderSpecPage(host, name));
    area.addEventListener('keydown', e => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); saveIt(); }
    });
    host.replaceChildren(bar, area);
    area.focus();
  };

  const renderSpecPage = (host, name, note) => {
    host.replaceChildren(el('p', { class: 'whint', text: 'Loading…' }));
    const path = 'specs/capabilities/' + name + '/spec.md';
    api('api/spec?path=' + encodeURIComponent(path)).then(r => {
      const back = el('a', { href: '#specs', class: 'wback', text: '← All specs' });
      if (r.error) {
        host.replaceChildren(back, el('p', { class: 'wbad', text: r.error }));
        return;
      }
      const raw = el('button', { type: 'button', class: 'wopen', text: 'raw' });
      raw.addEventListener('click', () => openFile(path, 0));
      const edit = el('button', { type: 'button', class: 'wopen', text: 'edit' });
      edit.addEventListener('click', () => openSpecEditor(host, name, path, r.text || ''));
      const bar = el('div', { class: 'wspecbar' }, [
        back,
        el('span', { class: 'wkind', text: r.path }),
        ...(note ? [el('span', { class: 'wspecstatus wok', text: note })] : []),
        raw,
        edit,
      ]);
      host.replaceChildren(bar, el('article', { class: 'specdoc', html: r.html }));
    }).catch(() => host.replaceChildren(el('p', { class: 'wbad', text: 'Could not read the spec.' })));
  };

  const buildWiki = () => {
    wikiBody.replaceChildren(el('p', { class: 'whint wbuilding', text: 'Building the wiki from the index…' }));
    api('api/wiki', {}).then(r => {
      if (r.exit === 0) wikiBody.replaceChildren(el('iframe', { class: 'wwikiframe', src: 'wiki/', title: 'Wiki' }));
      else wikiBody.replaceChildren(el('pre', { text: r.output }));
    }).catch(() => wikiBody.replaceChildren(el('p', { class: 'wbad', text: 'Wiki build failed.' })));
  };
  wikiBar.querySelector('.wmark').addEventListener('click', buildWiki);

  // The same chevron the map's own crumbs use, so every place reads alike.
  const chev = () => {
    const t = document.createElement('template');
    t.innerHTML = '<svg class="ico sep" viewBox="0 0 16 16" aria-hidden="true"><path d="M6 3.5 10.5 8 6 12.5"/></svg>';
    return t.content.firstChild;
  };
  const setCrumbs = (kind, name) => {
    const crumbs = document.getElementById('crumbs');
    if (!crumbs || !kind) return;
    const first = crumbs.querySelector('a');
    const stamp = crumbs.querySelector('.stamp');
    const ol = el('ol', {});
    const home = el('li', {});
    if (first) home.append(first.cloneNode(true));
    else home.append(el('a', { href: '#today', text: 'weave' }));
    home.append(chev());

    const here = el('li', {});
    if (kind === 'wiki') {
      here.append(el('span', { 'aria-current': 'page', text: 'Wiki' }));
      ol.append(home, here);
    } else if (name) {
      here.append(el('a', { href: '#specs', text: 'Specs' }));
      here.append(chev());
      const leaf = el('li', {});
      leaf.append(el('span', { 'aria-current': 'page', text: name }));
      ol.append(home, here, leaf);
    } else {
      here.append(el('span', { 'aria-current': 'page', text: 'Specs' }));
      ol.append(home, here);
    }
    crumbs.replaceChildren(ol);
    if (stamp) crumbs.append(stamp);
  };

  const showFull = (kind, name) => {
    if (!main) return;
    main.classList.add('wfull');
    specsPage.dataset.on = kind === 'specs' ? '1' : '0';
    wikiPage.dataset.on = kind === 'wiki' ? '1' : '0';
    if (kind === 'specs') {
      if (name) renderSpecPage(specsPage, name);
      else renderSpecs(specsPage);
    } else if (!wikiBody.querySelector('iframe') && !wikiBody.querySelector('pre')) {
      buildWiki();
    }
    setCrumbs(kind, name);
  };

  const hideFull = () => {
    if (!main) return;
    main.classList.remove('wfull');
    specsPage.dataset.on = '0';
    wikiPage.dataset.on = '0';
  };

  // The map owns #tabs; the two views are appended after Checks and restored whenever
  // the map re-renders the menu.
  const ensureTabs = () => {
    const nav = document.getElementById('tabs');
    if (!nav) return;
    if (!nav.querySelector('[data-wtab]')) {
      const icon = d => '<svg class="ico" viewBox="0 0 16 16" aria-hidden="true"><path d="' + d + '"/></svg>';
      nav.append(el('a', { href: '#specs', 'data-wtab': 'specs', html: icon('M4 2.5h5.5L12 5v8.5H4zM9.5 2.5V5H12M6 8h4M6 10.5h4') + '<span class="tl">Specs</span>' }));
      nav.append(el('a', { href: 'live', 'data-wtab': 'live', title: 'The map, lit up by your uncommitted change as you edit', html: icon('M1.5 8h3l2-4.5 3 9 2-4.5h3') + '<span class="tl">Live</span>' }));
      nav.append(el('a', { href: '#wiki', 'data-wtab': 'wiki', html: icon('M2.5 3.5h4a1.5 1.5 0 0 1 1.5 1.5v8a1.5 1.5 0 0 0-1.5-1.5h-4zM13.5 3.5h-4A1.5 1.5 0 0 0 8 5v8a1.5 1.5 0 0 1 1.5-1.5h4z') + '<span class="tl">Wiki</span>' }));
    }
    for (const a of nav.querySelectorAll('a')) {
      if (a.dataset.wtab) {
        if (a.dataset.wtab === activeFull) a.setAttribute('aria-current', 'page');
        else a.removeAttribute('aria-current');
      } else if (activeFull) {
        a.removeAttribute('aria-current');
      }
    }
  };
  const tabsNav = document.getElementById('tabs');
  if (tabsNav) new MutationObserver(ensureTabs).observe(tabsNav, { childList: true });
  ensureTabs();

  const route = () => {
    const h = location.hash;
    if (h === '#specs' || h.startsWith('#specs/') || h === '#wiki') {
      let name = '';
      if (h.startsWith('#specs/')) {
        activeFull = 'specs';
        try { name = decodeURIComponent(h.slice(7)); } catch (e) { name = ''; }
      } else {
        activeFull = h === '#wiki' ? 'wiki' : 'specs';
      }
      showFull(activeFull, name);
      ensureTabs();
      return;
    }
    const was = activeFull;
    activeFull = '';
    hideFull();
    ensureTabs();
    if (was === 'specs' && specsDirty) { specsDirty = false; location.reload(); }
  };

  /* ------------------------------------------------------- since you reviewed */
  const kindWord = k => ({ endpoint: 'endpoint', page: 'screen', module: 'module', table: 'table' }[k] || k);
  const reviewSection = data => {
    const wrap = el('section', { class: 'wreview', 'aria-label': 'Since you last reviewed' });
    wrap.append(el('h2', { text: 'Since you last reviewed' }));
    if (!data || data.error) { wrap.append(el('p', { class: 'whint', text: 'No git history to compare.' })); return wrap; }
    if (!data.files && !data.commits) {
      // Nothing to review is one quiet line, not a box that pushes the work down.
      const quiet = el('p', { class: 'wreview quiet' });
      quiet.append(el('b', { text: 'Since you last reviewed: ' }));
      quiet.append(document.createTextNode(data.mark ? 'nothing new since ' + data.mark.slice(0, 7) + '.' : 'nothing yet. Mark the page reviewed to start a baseline.'));
      return quiet;
    }
    const counts = {};
    (data.nodes || []).forEach(n => { counts[n.kind] = (counts[n.kind] || 0) + 1; });
    const parts = Object.entries(counts).map(([k, v]) => v + ' ' + kindWord(k) + (v === 1 ? '' : 's'));
    const verdict = el('p', { class: 'wverdict' });
    verdict.append(el('b', { text: data.commits + ' commit' + (data.commits === 1 ? '' : 's') + ' · ' + data.files + ' file' + (data.files === 1 ? '' : 's') }));
    verdict.append(document.createTextNode(parts.length ? ' → ' + parts.join(', ') + '. ' : ' → no contract change. '));
    if ((data.unproved || []).length) verdict.append(el('span', { class: 'wbad', text: (data.unproved.length) + ' unproved' }));
    else verdict.append(el('span', { class: 'wok', text: 'all proved' }));
    if ((data.outside || []).length) verdict.append(document.createTextNode(' · ' + data.outside.length + ' file(s) outside any change'));
    wrap.append(verdict);

    if ((data.nodes || []).length) {
      const ul = el('ul', { class: 'wrows' });
      for (const n of data.nodes.slice(0, 12)) {
        const a = el('a', { href: '#node/' + encodeURIComponent(n.id), class: 'wrow' });
        a.append(el('span', { class: 'wk', text: kindWord(n.kind) }));
        a.append(el('span', { class: 'wn', text: n.name }));
        a.append(el('span', { class: 'wf', text: (n.file || '').split('/').pop() }));
        ul.append(el('li', {}, [a]));
      }
      wrap.append(ul);
    }
    if ((data.tasks || []).length) {
      wrap.append(el('p', { class: 'whint', text: data.tasks.length + ' task(s) were ticked in this range.' }));
    }
    const mark = el('button', { type: 'button', class: 'wmark', text: 'Mark reviewed' });
    mark.addEventListener('click', () => {
      mark.disabled = true;
      api('api/review/mark', {}).then(r => {
        if (r.marked) { mark.textContent = 'Reviewed'; renderReview(); attribute(); }
        else { mark.disabled = false; mark.textContent = r.error || 'Could not mark'; }
      });
    });
    wrap.append(mark);
    return wrap;
  };

  let review = null;
  // The sheet goes under the Overview status line. Injecting it from
  // inside the mutation observer is what froze the page before; renderReview is now
  // called only from navigation and from the review fetch.
  const isToday = () => location.hash === '' || location.hash === '#today';
  const renderReview = () => {
    const detail = document.getElementById('detail');
    if (!detail) return;
    for (const old of detail.querySelectorAll('.wreviewslot')) old.remove();
    if (!isToday() || !review) return;
    // under the status line when Overview has a slot for it, else at the top
    const node = el('div', { class: 'wreviewslot' }, [reviewSection(review)]);
    const slot = document.getElementById('reviewslot');
    if (slot) slot.append(node); else detail.prepend(node);
  };
  const loadReview = () => api('api/review').then(r => { review = r; renderReview(); }).catch(() => {});

  // the review sheet belongs to Today only, and Today is re-rendered on navigation
  const attribute = () => {
    renderReview();
    if (isToday()) loadReview();
  };
  // hashchange fires on window; a listener on document never sees it.
  window.addEventListener('hashchange', () => { attribute(); route(); });

  /* an Open button beside every path chip the page already renders */
  const addOpeners = () => {
    for (const chip of document.querySelectorAll('.path')) {
      if (chip.nextElementSibling && chip.nextElementSibling.classList.contains('wopen')) continue;
      const open = el('button', { class: 'wopen', type: 'button', text: 'open' });
      open.addEventListener('click', ev => { ev.preventDefault(); ev.stopPropagation(); openFile(chip.dataset.path, 0); });
      chip.after(open);
    }
  };

  /* the same tick, where a change page states the next task: the panel is the only
     surface that can write, so the hosted page gets the action the static one cannot */
  const changeSlug = () => {
    const h = location.hash || '';
    if (h.indexOf('#node/') !== 0) return '';
    let id = '';
    try { id = decodeURIComponent(h.slice(6)); } catch (e) { return ''; }
    return id.indexOf('change:') === 0 ? id.slice(7) : '';
  };

  const addTaskTicks = () => {
    const slug = changeSlug();
    if (!slug) return;
    const detail = document.getElementById('detail');
    if (!detail) return;
    for (const li of detail.querySelectorAll('.tasks > li')) {
      if (li.querySelector('.wtick')) continue;
      const ring = li.querySelector('.ring[data-k="task"]');
      const tid = li.querySelector('.tid');
      if (!ring || !tid) continue;
      const done = !!(li.parentElement && li.parentElement.classList.contains('done'));
      ring.remove();
      li.classList.add('wtickrow');
      li.prepend(tickButton(slug, { id: tid.textContent.trim(), done }, () => location.reload()));
    }
    const nt = detail.querySelector('.nexttask .nt1');
    if (nt && !nt.querySelector('.wtick')) {
      const tid = nt.querySelector('.tid');
      if (tid) nt.prepend(tickButton(slug, { id: tid.textContent.trim(), done: false }, () => location.reload()));
    }
  };

  const enhance = () => { addOpeners(); addTaskTicks(); };
  enhance();
  // Only the Open buttons and the change-page ticks need the observer; the review
  // sheet has its own slot.
  new MutationObserver(enhance).observe(document.getElementById('main') || document.body, { childList: true, subtree: true });

  /* first paint: check once, load the sheet, then re-check when the tab returns */
  runCheck(false);
  attribute();
  route();
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && Date.now() - checkedAt > 30000) runCheck(false);
  });
})();
