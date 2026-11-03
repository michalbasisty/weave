/* The hosted layer. Everything talks to /api/* on this origin; the token is injected
   by the server and exists only in this document. */
(() => {
  const token = (window.__weave && window.__weave.token) || '';
  const api = (path, body) => fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Weave-Token': token },
    body: body === undefined ? undefined : JSON.stringify(body),
  }).then(r => r.json());

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
  const btnWiki = el('button', { class: 'wact', type: 'button', title: 'Regenerate the wiki and open it' }, [
    el('span', { text: 'Wiki' }),
  ]);
  const bar = el('div', { class: 'wbar', role: 'toolbar', 'aria-label': 'Actions' }, [gatePill, btnWiki]);
  const theme = document.getElementById('theme');
  if (theme && theme.parentElement) theme.parentElement.insertBefore(bar, theme);
  else document.body.append(bar);

  /* ---------------------------------------------------------------- drawer */
  const dialog = el('dialog', { class: 'wdialog', 'aria-label': 'Panel' });
  const dTitle = el('b', { text: '' });
  const dKind = el('span', { class: 'wkind', text: '' });
  const dClose = el('button', { type: 'button', title: 'Close', 'aria-label': 'Close' }, [el('span', { text: '×' })]);
  const dBody = el('div', { class: 'wbody' });
  dialog.append(el('header', {}, [dTitle, dKind, dClose]), dBody);
  document.body.append(dialog);

  const openDrawer = (title, kind, nodes) => {
    dTitle.textContent = title;
    dTitle.title = title;
    dKind.textContent = kind || '';
    dBody.replaceChildren(...nodes);
    if (!dialog.open) dialog.showModal();
    dClose.focus();
  };
  dClose.addEventListener('click', () => dialog.close());
  dialog.addEventListener('click', e => { if (e.target === dialog) dialog.close(); });
  // Escape belongs to the dialog while it is open: the page's own Escape clears the
  // selection, which would remove the item you were reading.
  dialog.addEventListener('keydown', e => { if (e.key === 'Escape') e.stopPropagation(); }, true);

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
    api('/api/file?path=' + encodeURIComponent(path) + (line ? '&line=' + line : ''))
      .then(res => {
        if (res.error) { openDrawer(path, 'file', [el('p', { class: 'wbad', text: res.error })]); return; }
        const pre = el('pre', {});
        pre.append(linkify(res.text));
        const note = el('div', { class: 'wnote' });
        const input = el('input', { type: 'text', placeholder: 'note for this file — why it is like this', 'aria-label': 'Note' });
        const save = el('button', { type: 'button', text: 'Remember' });
        save.addEventListener('click', () => {
          const text = input.value.trim();
          if (!text) return;
          save.disabled = true;
          api('/api/note', { note: text, files: [path] }).then(r => {
            save.disabled = false;
            input.value = '';
            dBody.prepend(el('p', { class: r.exit === 0 ? 'wok' : 'wbad', text: r.exit === 0 ? 'Saved. context() returns it as a note, not evidence.' : r.output }));
          });
        });
        note.append(input, save);
        openDrawer(res.path, 'lines ' + res.start + '–' + (res.start + res.text.split('\n').length - 1) + ' of ' + res.total, [pre, note]);
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
    api('/api/check', {}).then(r => {
      checkedAt = Date.now();
      const bad = r.exit !== 0;
      setGate(bad ? 'bad' : 'ok', bad ? 'contract broken' : 'checked ' + Math.round((Date.now() - t0) / 100) / 10 + 's');
      if (!show) return;
      const pre = el('pre', {});
      pre.append(linkify(r.output || '(no output)'));
      openDrawer('weave check', 'working tree', [
        el('p', { class: bad ? 'wbad' : 'wok', text: bad ? 'Exit ' + r.exit + ' — the contract is broken.' : 'Exit 0 — every call matches an endpoint.' }),
        pre,
      ]);
    }).catch(() => setGate('bad', 'check failed'));
  };
  gatePill.addEventListener('click', () => runCheck(true));
  btnWiki.addEventListener('click', () => {
    btnWiki.disabled = true;
    btnWiki.textContent = 'Building…';
    api('/api/wiki', {}).then(r => {
      btnWiki.disabled = false;
      btnWiki.textContent = 'Wiki';
      if (r.exit === 0) window.open(r.url, '_blank');
      else openDrawer('weave wiki', 'failed', [el('pre', { text: r.output })]);
    });
  });

  /* ------------------------------------------------------- since you reviewed */
  const kindWord = k => ({ endpoint: 'endpoint', page: 'screen', module: 'module', table: 'table' }[k] || k);
  const reviewSection = data => {
    const wrap = el('section', { class: 'wreview', 'aria-label': 'Since you last reviewed' });
    wrap.append(el('h2', { text: 'Since you last reviewed' }));
    if (!data || data.error) { wrap.append(el('p', { class: 'whint', text: 'No git history to compare.' })); return wrap; }
    if (!data.files && !data.commits) {
      wrap.append(el('p', { class: 'whint', text: 'Nothing new since ' + (data.mark || '').slice(0, 7) + '.' }));
      return wrap;
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
      api('/api/review/mark', {}).then(r => {
        if (r.marked) { mark.textContent = 'Reviewed'; renderReview(); attribute(); }
        else { mark.disabled = false; mark.textContent = r.error || 'Could not mark'; }
      });
    });
    wrap.append(mark);
    return wrap;
  };

  let review = null;
  // The sheet goes at the top of the detail pane, before the hero. Injecting it from
  // inside the mutation observer is what froze the page before; renderReview is now
  // called only from navigation and from the review fetch.
  const isToday = () => location.hash === '' || location.hash === '#today';
  const renderReview = () => {
    const detail = document.getElementById('detail');
    if (!detail) return;
    for (const old of detail.querySelectorAll(':scope > .wreviewslot')) old.remove();
    if (!isToday() || !review) return;
    detail.prepend(el('div', { class: 'wreviewslot' }, [reviewSection(review)]));
  };
  const loadReview = () => api('/api/review').then(r => { review = r; renderReview(); }).catch(() => {});

  // the review sheet belongs to Today only, and Today is re-rendered on navigation
  const attribute = () => {
    renderReview();
    if (isToday()) loadReview();
  };
  // hashchange fires on window; a listener on document never sees it.
  window.addEventListener('hashchange', () => { if (dialog.open) dialog.close(); attribute(); });

  /* an Open button beside every path chip the page already renders */
  const addOpeners = () => {
    for (const chip of document.querySelectorAll('.path')) {
      if (chip.nextElementSibling && chip.nextElementSibling.classList.contains('wopen')) continue;
      const open = el('button', { class: 'wopen', type: 'button', text: 'open' });
      open.addEventListener('click', ev => { ev.preventDefault(); ev.stopPropagation(); openFile(chip.dataset.path, 0); });
      chip.after(open);
    }
  };
  addOpeners();
  // Only the Open buttons need the observer; the review sheet has its own slot.
  new MutationObserver(addOpeners).observe(document.getElementById('main') || document.body, { childList: true, subtree: true });

  /* first paint: check once, load the sheet, then re-check when the tab returns */
  runCheck(false);
  attribute();
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && Date.now() - checkedAt > 30000) runCheck(false);
  });
})();
