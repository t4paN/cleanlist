let cats = JSON.parse(JSON.stringify(CATEGORIES));

const $ = (id) => document.getElementById(id);
const el = (tag, cls, txt) => {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (txt !== undefined) n.textContent = txt;
  return n;
};

function showErr(msg) {
  $('ok').style.display = 'none';
  const e = $('err');
  if (!msg) { e.style.display = 'none'; return; }
  e.textContent = msg;
  e.style.display = 'block';
}

function slug(s) {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') ||
    'cat-' + Date.now().toString(36);
}

// A fixed, uneditable day — arrival and departure are never a choice.
function fixedDay(label, value) {
  const d = el('div', 'day fixed');
  d.appendChild(el('span', 'lbl', label));
  d.appendChild(el('span', 'val', value));
  return d;
}

function markerSelect(current, onChange) {
  const s = el('select');
  MARKERS.forEach((m) => {
    const o = el('option', null, m);
    o.value = m;
    if (m === current) o.selected = true;
    s.appendChild(o);
  });
  s.onchange = () => onChange(s.value);
  return s;
}

// Renders a stay as a labelled strip of days rather than abstract day numbers,
// so nobody has to work out whether day 1 is the arrival.
function shortStrip(cat, nights) {
  const strip = el('div', 'strip');
  const table = cat.short_stay[nights] || (cat.short_stay[nights] = {});
  const n = parseInt(nights, 10);

  strip.appendChild(fixedDay('Day 1', 'AF'));
  for (let day = 2; day <= n; day++) {
    const d = el('div', 'day');
    d.appendChild(el('span', 'lbl', 'Day ' + day));
    const key = String(day);
    d.appendChild(markerSelect(table[key] || 'F', (v) => { table[key] = v; }));
    strip.appendChild(d);
  }
  strip.appendChild(fixedDay('Day ' + (n + 1), 'AN'));
  return strip;
}

// The long-stay preview is resolved server-side rather than reimplemented here.
// Two copies of the rule would eventually disagree, and the printed sheet is
// the one that matters.
async function refreshPreview(cat, target) {
  try {
    const r = await fetch('/api/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ category: cat, max_days: 14 }),
    });
    const data = await r.json();
    target.innerHTML = '';
    (data.long || []).forEach((m, i) => {
      const d = el('div', 'day fixed');
      d.appendChild(el('span', 'lbl', 'Day ' + (i + 1)));
      d.appendChild(el('span', 'val', m));
      target.appendChild(d);
    });
  } catch (e) {
    target.textContent = 'preview unavailable';
  }
}

function render() {
  const list = $('list');
  list.innerHTML = '';

  cats.forEach((cat, idx) => {
    const card = el('div', 'cat-card');
    const head = el('div', 'cat-head');

    const nameF = el('label', 'field');
    nameF.appendChild(el('span', null, 'Name'));
    const name = el('input');
    name.value = cat.label;
    name.oninput = () => { cat.label = name.value; };
    nameF.appendChild(name);
    head.appendChild(nameF);

    head.appendChild(el('div', 'spacer'));
    const del = el('button', 'danger', 'Delete');
    del.onclick = () => {
      if (!confirm(`Delete category "${cat.label}"?`)) return;
      cats.splice(idx, 1);
      render();
    };
    head.appendChild(del);
    card.appendChild(head);

    // ----- Long stay -----
    card.appendChild(el('div', 'sub', 'Repeating interval — any stay length not listed below'));
    const lrow = el('div', 'row');
    const preview = el('div', 'strip');

    const mkNum = (label, val, set) => {
      const f = el('label', 'field');
      f.style.marginBottom = '0';
      f.appendChild(el('span', null, label));
      const i = el('input');
      i.type = 'number';
      i.min = '1';
      i.value = val;
      i.style.width = '80px';
      i.oninput = () => { set(parseInt(i.value, 10) || 1); refreshPreview(cat, preview); };
      f.appendChild(i);
      return f;
    };

    lrow.appendChild(mkNum('First change on day', cat.long_stay.first_change_day,
      (v) => { cat.long_stay.first_change_day = v; }));
    lrow.appendChild(mkNum('then every (days)', cat.long_stay.interval,
      (v) => { cat.long_stay.interval = v; }));

    const mf = el('label', 'field');
    mf.style.marginBottom = '0';
    mf.appendChild(el('span', null, 'Service'));
    mf.appendChild(markerSelect(cat.long_stay.marker, (v) => {
      cat.long_stay.marker = v;
      refreshPreview(cat, preview);
    }));
    lrow.appendChild(mf);
    card.appendChild(lrow);

    card.appendChild(el('div', 'sub', 'Preview — a 13-night stay'));
    card.appendChild(preview);
    refreshPreview(cat, preview);

    // ----- Short stays -----
    card.appendChild(el('div', 'sub', 'Short stays — these override the interval entirely'));

    Object.keys(cat.short_stay)
      .map(Number)
      .sort((a, b) => a - b)
      .forEach((n) => {
        const wrap = el('div', 'short-row');
        const hdr = el('div', 'row');
        hdr.appendChild(el('b', null, n + ' night' + (n === 1 ? '' : 's')));
        hdr.appendChild(el('div', 'spacer'));
        const rm = el('button', null, 'Remove');
        rm.onclick = () => { delete cat.short_stay[String(n)]; render(); };
        hdr.appendChild(rm);
        wrap.appendChild(hdr);
        wrap.appendChild(shortStrip(cat, String(n)));
        card.appendChild(wrap);
      });

    const addShort = el('button', null, '+ Add stay length');
    addShort.style.marginTop = '10px';
    addShort.onclick = () => {
      const v = prompt('How many nights?');
      if (!v) return;
      const n = parseInt(v, 10);
      if (!n || n < 1 || n > 60) { showErr('Nights must be between 1 and 60.'); return; }
      if (cat.short_stay[String(n)]) { showErr(n + ' nights is already listed.'); return; }
      cat.short_stay[String(n)] = {};
      render();
    };
    card.appendChild(addShort);

    list.appendChild(card);
  });
}

$('btn-add').onclick = () => {
  const label = prompt('Category name (e.g. the agency name)');
  if (!label) return;
  cats.push({
    id: slug(label),
    label,
    long_stay: { first_change_day: 3, interval: 3, marker: 'S & P' },
    short_stay: { 2: {}, 3: { 3: 'S' }, 4: { 3: 'S & P' } },
  });
  render();
};

$('btn-save').onclick = async () => {
  showErr('');
  try {
    const r = await fetch('/api/categories', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ categories: cats }),
    });
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || 'save failed');
    $('ok').style.display = 'block';
    setTimeout(() => { $('ok').style.display = 'none'; }, 2500);
  } catch (e) {
    showErr(e.message);
  }
};

render();
