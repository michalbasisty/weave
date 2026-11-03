/* Actions for the hosted page. Everything here talks to /api/* on the same origin;
   the token is injected by the server and only exists in this document. */
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

  /* ---------------------------------------------------------------- toolbar */
  const bar = el('div', { class: 'wbar', role: 'toolbar', 'aria-label': 'Actions' });
  const dot = el('span', { class: 'wdot', id: 'wdot', title: 'gate' });
  const btnCheck = el('button', { type: 'button', title: 'Run weave check on the working tree' }, [dot, el('span', { text: 'Check' })]);
  const btnIndex = el('button', { type: 'button', title: 'Reindex and redraw' }, [el('span', { text: 'Reindex' })]);
  const btnWiki = el('button', { type: 'button', title: 'Regenerate the wiki and open it' }, [el('span', { text: 'Wiki' })]);
  bar.append(btnCheck, btnIndex, btnWiki);
  const theme = document.getElementById('theme');
  if (theme && theme.parentElement) theme.parentElement.insertBefore(bar, theme);
  else document.body.append(bar);

  /* ---------------------------------------------------------------- drawer */
  const drawer = el('aside', { class: 'wdrawer', hidden: '', role: 'dialog', 'aria-label': 'Panel' });
  const dTitle = el('b', { text: '' });
  const dKind = el('span', { class: 'wkind', text: '' });
  const dClose = el('button', { type: 'button', title: 'Close', 'aria-label': 'Close' }, [el('span', { text: '×' })]);
  const dBody = el('div', { class: 'wbody' });
  const dHead = el('header', {}, [dTitle, dKind, dClose]);
  drawer.append(dHead, dBody);
  document.body.append(drawer);

  const openDrawer = (title, kind, nodes) => {
    dTitle.textContent = title;
    dTitle.title = title;
    dKind.textContent = kind || '';
    dBody.replaceChildren(...nodes);
    drawer.hidden = false;
    dClose.focus();
  };
  dClose.addEventListener('click', () => { drawer.hidden = true; });

  const busy = (on, label) => {
    dot.className = 'wdot' + (on ? ' busy' : '');
    if (label) btnCheck.querySelector('span').textContent = label;
  };

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

  /* ---------------------------------------------------------------- actions */
  const runCheck = () => {
    busy(true, 'Checking…');
    api('/api/check', {}).then(r => {
      busy(false, 'Check');
      const bad = r.exit !== 0;
      dot.className = 'wdot' + (bad ? ' bad' : '');
      const pre = el('pre', {});
      pre.append(linkify(r.output || '(no output)'));
      const head = bad
        ? el('p', { class: 'wbad', text: 'Exit ' + r.exit + ' — the contract is broken.' })
        : el('p', { class: 'wok', text: 'Exit 0 — every call matches an endpoint.' });
      openDrawer('weave check', 'working tree', [head, pre]);
    });
  };
  btnCheck.addEventListener('click', runCheck);

  btnIndex.addEventListener('click', () => {
    btnIndex.disabled = true;
    api('/api/reindex', {}).then(() => location.reload());
  });

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

  /* an Open button beside every path chip the page already renders */
  const addOpeners = () => {
    for (const chip of document.querySelectorAll('.path')) {
      if (chip.nextElementSibling && chip.nextElementSibling.classList.contains('wopen')) continue;
      const open = el('button', { class: 'wopen', type: 'button', text: 'open' });
      open.addEventListener('click', ev => {
        ev.preventDefault();
        ev.stopPropagation();
        openFile(chip.dataset.path, 0);
      });
      chip.after(open);
    }
  };
  addOpeners();
  new MutationObserver(addOpeners).observe(document.getElementById('main') || document.body, { childList: true, subtree: true });

  /* the gate state on arrival, so the dot is not decoration */
  api('/api/status').then(s => {
    if (!s || s.error) return;
    dot.title = (s.counts.endpoint || 0) + ' endpoints · built ' + (s.builtAt || '');
  });
})();
