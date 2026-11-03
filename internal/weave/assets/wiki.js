/* Search and orientation for the generated wiki. No dependencies, works from file://. */
(() => {
  const input = document.getElementById('q');
  const box = document.getElementById('hits');
  const count = document.getElementById('hitcount');
  if (!input || !box) return;

  // search.js sits at the site root. Resolving every indexed path against its own
  // URL is what makes a result work from s/, e/, m/, t/ and c/ alike, not only from
  // the home page.
  const self = document.currentScript ? document.currentScript.src : '';
  const root = self ? self.slice(0, self.lastIndexOf('/') + 1) : '';

  // mark the current page in the sidebar and keep it in view
  const active = document.querySelector('.side a[aria-current="page"]');
  if (active && active.closest('.side')) {
    const side = active.closest('.side');
    side.scrollTop = Math.max(0, active.offsetTop - side.clientHeight / 2);
  }

  const items = Array.isArray(window.W) ? window.W : [];
  if (items.length) input.placeholder = 'Search ' + items.length + ' pages — /';
  let shown = [];
  let cursor = 0;

  const say = msg => { if (count) count.textContent = msg; };

  const paint = q => {
    box.replaceChildren();
    if (!q) {
      box.hidden = true;
      input.setAttribute('aria-expanded', 'false');
      say('');
      return;
    }
    if (!shown.length) {
      const none = document.createElement('p');
      none.className = 'none';
      none.textContent = 'No page matches “' + q + '”. Try an endpoint path, a screen route, a change name or a table.';
      box.append(none);
      box.hidden = false;
      input.setAttribute('aria-expanded', 'false');
      say('No results');
      return;
    }
    box.append(...shown.map((hit, i) => {
      const a = document.createElement('a');
      a.href = root + hit[2];
      a.setAttribute('role', 'option');
      a.setAttribute('aria-selected', String(i === cursor));
      const k = document.createElement('span');
      k.className = 'k';
      k.textContent = hit[1];
      const n = document.createElement('span');
      n.className = 'n';
      n.textContent = hit[0];
      const h = document.createElement('span');
      h.className = 'h';
      h.textContent = hit[3] || '';
      a.append(k, n, h);
      return a;
    }));
    box.hidden = false;
    input.setAttribute('aria-expanded', 'true');
    say(shown.length + (shown.length === 1 ? ' result' : ' results'));
    const sel = box.querySelector('[aria-selected="true"]');
    if (sel && sel.scrollIntoView) sel.scrollIntoView({ block: 'nearest' });
  };

  const search = q => {
    const needle = q.trim().toLowerCase();
    shown = needle ? items.filter(h => h[0].toLowerCase().includes(needle)).slice(0, 20) : [];
    cursor = 0;
    paint(needle);
  };

  input.addEventListener('input', () => search(input.value));
  input.addEventListener('focus', () => { if (input.value) search(input.value); });
  input.addEventListener('keydown', e => {
    if (e.key === 'ArrowDown' && shown.length) { e.preventDefault(); cursor = (cursor + 1) % shown.length; paint(input.value.trim().toLowerCase()); }
    else if (e.key === 'ArrowUp' && shown.length) { e.preventDefault(); cursor = (cursor - 1 + shown.length) % shown.length; paint(input.value.trim().toLowerCase()); }
    else if (e.key === 'Enter' && shown.length) { e.preventDefault(); location.href = root + shown[cursor][2]; }
    else if (e.key === 'Escape') { input.value = ''; search(''); input.blur(); }
  });
  document.addEventListener('click', e => {
    if (!box.contains(e.target) && e.target !== input) { box.hidden = true; }
  });
  document.addEventListener('keydown', e => {
    if (e.key === '/' && document.activeElement !== input) { e.preventDefault(); input.focus(); input.select(); }
  });
})();
