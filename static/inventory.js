// Selection holds both kinds of target. A named item is identified by its id
// alone; a per-room item needs the room too, because the room is its identity.
const sel = new Map(); // key -> {item, room|null, loan}
let items = JSON.parse(JSON.stringify(ITEMS));

const $ = (id) => document.getElementById(id);
const el = (tag, cls, txt) => {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (txt !== undefined) n.textContent = txt;
  return n;
};

function showErr(msg) {
  const e = $('err');
  if (!msg) { e.style.display = 'none'; return; }
  e.textContent = msg;
  e.style.display = 'block';
}

async function post(url, body) {
  const r = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body || {}),
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || 'request failed');
  return data;
}

const refresh = () => { location.href = '/inventory?date=' + $('date').value; };

function keyOf(item, room) { return room ? item + '|' + room : item; }

function paint() {
  document.querySelectorAll('[data-item]').forEach((n) => {
    n.classList.toggle('sel', sel.has(keyOf(n.dataset.item, n.dataset.room)));
  });
  const n = sel.size;
  $('count').textContent = n + ' selected';
  $('btn-clear').disabled = n === 0;

  // Lending needs things that are in; returning needs things that are out. A
  // mixed selection can do neither, so both buttons go dark.
  const vals = [...sel.values()];
  const allIn = n > 0 && vals.every((v) => !v.loan);
  const allOut = n > 0 && vals.every((v) => v.loan);
  // Per-room selections already carry their room; named ones need one chosen,
  // and mixing the two would make that ambiguous.
  const rooms = new Set(vals.map((v) => (v.room ? 'room' : 'named')));
  $('btn-lend').disabled = !(allIn && rooms.size === 1);
  $('btn-return').disabled = !allOut;
}

function bindSelect(node) {
  node.addEventListener('click', () => {
    const item = node.dataset.item;
    const room = node.dataset.room || null;
    const key = keyOf(item, room);
    if (sel.has(key)) sel.delete(key);
    else sel.set(key, { item, room, loan: node.dataset.loan });
    paint();
  });
}
document.querySelectorAll('.unit.named, .roomcell').forEach(bindSelect);

$('btn-clear').onclick = () => { sel.clear(); paint(); };
$('date').onchange = refresh;
$('btn-print').onclick = () => window.open('/inventory/print?date=' + $('date').value, '_blank');
$('btn-undo').onclick = async () => {
  try { await post('/api/undo'); refresh(); } catch (e) { showErr(e.message); }
};

// --- Lend ---
$('btn-lend').onclick = () => {
  const vals = [...sel.values()];
  const perRoom = !!vals[0].room;
  // Per-room items already know their room, so the picker is hidden.
  $('lend-roomfield').style.display = perRoom ? 'none' : '';
  const label = (id) => {
    const it = items.find((x) => x.id === id);
    return it ? it.label : id;
  };
  $('lend-what').textContent = perRoom
    ? label(vals[0].item) + ' — δωμάτια ' + vals.map((v) => v.room).join(', ')
    : vals.map((v) => label(v.item)).join(', ');
  $('lend-on').value = $('date').value;
  $('lend-due').value = '';
  $('lend-note').value = '';
  $('dlg-lend').showModal();
};
$('lend-cancel').onclick = () => $('dlg-lend').close();
$('lend-ok').onclick = async () => {
  const vals = [...sel.values()];
  const room = $('lend-room').value;
  try {
    await post('/api/lend', {
      loans: vals.map((v) => ({ item: v.item, room: v.room || room })),
      lent_on: $('lend-on').value,
      due_on: $('lend-due').value,
      note: $('lend-note').value,
    });
    $('dlg-lend').close();
    refresh();
  } catch (e) {
    $('dlg-lend').close();
    showErr(e.message);
  }
};

// --- Return ---
$('btn-return').onclick = async () => {
  try {
    await post('/api/return', {
      ids: [...sel.values()].map((v) => v.loan),
      date: $('date').value,
    });
    refresh();
  } catch (e) { showErr(e.message); }
};

document.querySelectorAll('button.ret').forEach((b) => {
  b.onclick = async (e) => {
    e.stopPropagation();
    try {
      await post('/api/return', { ids: [b.dataset.id], date: $('date').value });
      refresh();
    } catch (err) { showErr(err.message); }
  };
});

// --- Items editor ---
function slug(s) {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') ||
    'item-' + Date.now().toString(36);
}

function svgFor(path) {
  return '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
    'stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' + path + '</svg>';
}

function renderItems() {
  const list = $('items-list');
  list.innerHTML = '';
  items.forEach((t, idx) => {
    const card = el('div', 'short-row');
    const row = el('div', 'row');

    const nf = el('label', 'field');
    nf.style.marginBottom = '0';
    nf.appendChild(el('span', null, 'Name'));
    const name = el('input');
    name.value = t.label;
    name.oninput = () => { t.label = name.value; };
    nf.appendChild(name);
    row.appendChild(nf);

    const rf = el('label', 'field');
    rf.style.marginBottom = '0';
    rf.appendChild(el('span', null, 'Due back'));
    const rs = el('select');
    RETURN_RULES.forEach((r) => {
      const o = el('option', null, RULE_LABELS[r] || r);
      o.value = r;
      if (r === t.return_rule) o.selected = true;
      rs.appendChild(o);
    });
    rs.onchange = () => { t.return_rule = rs.value; renderItems(); };
    rf.appendChild(rs);
    row.appendChild(rf);

    if (t.return_rule === 'days') {
      const df = el('label', 'field');
      df.style.marginBottom = '0';
      df.appendChild(el('span', null, 'Days'));
      const di = el('input');
      di.type = 'number';
      di.min = '0';
      di.style.width = '70px';
      di.value = t.return_days || 0;
      di.oninput = () => { t.return_days = parseInt(di.value, 10) || 0; };
      df.appendChild(di);
      row.appendChild(df);
    }

    // Icon picker: a row of the built-in set, since a free-text field would
    // just be a way to type an id that renders nothing.
    const ip = el('div', 'icon-pick');
    ICONS.forEach((ic) => {
      const b = el('button');
      b.type = 'button';
      b.title = ic.Name;
      b.innerHTML = svgFor(ic.Path);
      if ((t.icon || '') === ic.ID) b.classList.add('on');
      b.onclick = () => { t.icon = ic.ID; renderItems(); };
      ip.appendChild(b);
    });

    const pf = el('label', 'row');
    pf.style.gap = '5px';
    const pc = el('input');
    pc.type = 'checkbox';
    pc.checked = !!t.per_room;
    pc.onchange = () => { t.per_room = pc.checked; };
    pf.appendChild(pc);
    pf.appendChild(el('span', 'count', 'Per room'));
    row.appendChild(pf);

    row.appendChild(el('div', 'spacer'));
    const del = el('button', 'danger', 'Delete');
    del.onclick = () => {
      if (!confirm('Delete "' + t.label + '"?')) return;
      items.splice(idx, 1);
      renderItems();
    };
    row.appendChild(del);

    card.appendChild(row);
    card.appendChild(ip);
    list.appendChild(card);
  });
}

$('btn-items').onclick = (e) => {
  e.preventDefault();
  items = JSON.parse(JSON.stringify(ITEMS));
  renderItems();
  $('dlg-items').showModal();
};
$('items-cancel').onclick = () => $('dlg-items').close();
$('items-add').onclick = () => {
  const label = prompt('Item name (include the number, e.g. "Σίδερο 5")');
  if (!label) return;
  // Icon left blank on purpose: the server guesses one from the name, and the
  // picker below can override it.
  items.push({ id: slug(label), label, return_rule: 'same_day', return_days: 0, per_room: false, icon: '' });
  renderItems();
};
$('items-save').onclick = async () => {
  try {
    await post('/api/items', { items });
    $('dlg-items').close();
    refresh();
  } catch (e) {
    $('dlg-items').close();
    showErr(e.message);
  }
};

paint();
