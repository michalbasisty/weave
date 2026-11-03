/* Search and orientation for the generated wiki. No dependencies, works from file://. */
(() => {
  const input = document.getElementById('q');
  const box = document.getElementById('hits');
  if (!input || !box) return;

  // mark the current page in the sidebar and keep it in view
  const active = document.querySelector('.side a[aria-current="page"]');
  if (active && active.scrollIntoView) {
    const side = active.closest('.side');
    if (side) side.scrollTop = Math.max(0, active.offsetTop - side.clientHeight / 2);
  }

  const items = Array.isArray(window.W) ? window.W : [];
  let shown = [];
  let cursor = 0;

  const paint = () => {
    if (!shown.length) { box.hidden = true; box.replaceChildren(); return; }
    box.replaceChildren(...shown.map((hit, i) => {
      const a = document.createElement('a');
      a.href = hit[2];
      a.setAttribute('role', 'option');
      a.setAttribute('aria-selected', String(i === cursor));
      const k = document.createElement('span');
      k.className = 'k';
      k.textContent = hit[1];
      const n = document.createElement('span');
      n.textContent = hit[0];
      a.append(k, n);
      return a;
    }));
    box.hidden = false;
    const sel = box.querySelector('[aria-selected="true"]');
    if (sel && sel.scrollIntoView) sel.scrollIntoView({ block: 'nearest' });
  };

  const search = q => {
    const needle = q.trim().toLowerCase();
    if (!needle) { shown = []; cursor = 0; paint(); return; }
    shown = items.filter(h => h[0].toLowerCase().includes(needle)).slice(0, 20);
    cursor = 0;
    paint();
  };

  input.addEventListener('input', () => search(input.value));
  input.addEventListener('focus', () => { if (input.value) search(input.value); });
  input.addEventListener('keydown', e => {
    if (e.key === 'ArrowDown' && shown.length) { e.preventDefault(); cursor = (cursor + 1) % shown.length; paint(); }
    else if (e.key === 'ArrowUp' && shown.length) { e.preventDefault(); cursor = (cursor - 1 + shown.length) % shown.length; paint(); }
    else if (e.key === 'Enter' && shown.length) { e.preventDefault(); location.href = shown[cursor].u; }
    else if (e.key === 'Escape') { input.value = ''; search(''); input.blur(); }
  });
  document.addEventListener('click', e => {
    if (!box.contains(e.target) && e.target !== input) { box.hidden = true; }
  });
  document.addEventListener('keydown', e => {
    if (e.key === '/' && document.activeElement !== input) { e.preventDefault(); input.focus(); input.select(); }
  });
})();
