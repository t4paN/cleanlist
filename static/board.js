const sel = new Set();
let lastClicked = null;

const $ = (id) => document.getElementById(id);
const rooms = () => [...document.querySelectorAll('.room')];

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

function refresh() {
  location.href = '/?date=' + $('date').value;
}

// Dates are held as ISO everywhere a machine reads them and shown in the
// hotel's format everywhere a person does. The formatting is always the
// server's — the date picker carries the rendered version of its own value in
// data-gr, and changing the picker reloads the page, so the two never drift.
const shownDate = () => $('date').dataset.gr || $('date').value;

function paint() {
  rooms().forEach((el) => el.classList.toggle('sel', sel.has(el.dataset.room)));
  const n = sel.size;
  $('count').textContent = n + ' selected';
  $('btn-in').disabled = n === 0;
  $('btn-out').disabled = n === 0;
  $('btn-clear').disabled = n === 0;
  n === 1 ? loadDetail([...sel][0]) : ($('detail').style.display = 'none');
}

// Shift-click selects a range, but only within one section — the columns are
// separate sheets and a range spanning them is never what anyone means.
function clickRoom(e, el) {
  const room = el.dataset.room;
  const section = el.closest('.section');
  if (e.shiftKey && lastClicked && lastClicked.closest('.section') === section) {
    const list = [...section.querySelectorAll('.room')];
    const a = list.indexOf(lastClicked);
    const b = list.indexOf(el);
    list.slice(Math.min(a, b), Math.max(a, b) + 1)
      .forEach((r) => sel.add(r.dataset.room));
  } else {
    sel.has(room) ? sel.delete(room) : sel.add(room);
    lastClicked = el;
  }
  paint();
}

// The server orders these — current stay first, then newest arrival down — and
// says which one is current, so the panel matches the date the board is showing.
async function loadDetail(room) {
  const r = await fetch('/api/room?room=' + encodeURIComponent(room)
    + '&date=' + encodeURIComponent($('date').value));
  const { stays, labels } = await r.json();
  $('detail-room').textContent = room;
  const body = $('detail-body');
  body.innerHTML = '';
  if (!stays.length) {
    body.innerHTML = '<tr><td colspan="4" style="color:var(--dim)">No stays recorded.</td></tr>';
  }
  stays.forEach((s) => {
    const tr = document.createElement('tr');
    if (s.current) tr.className = 'now';
    [labels[s.category] || s.category, s.arrival_gr, s.departure_gr].forEach((v) => {
      const td = document.createElement('td');
      td.textContent = v;
      tr.appendChild(td);
    });
    const td = document.createElement('td');
    const b = document.createElement('button');
    b.className = 'danger';
    b.textContent = 'Remove';
    b.onclick = async () => {
      if (!confirm(`Remove the stay ${s.arrival_gr} → ${s.departure_gr} in room ${room}?`)) return;
      try {
        await post('/api/stay/delete', { room, id: s.id });
        refresh();
      } catch (err) { showErr(err.message); }
    };
    td.appendChild(b);
    tr.appendChild(td);
    body.appendChild(tr);
  });
  $('detail').style.display = 'block';
}

rooms().forEach((el) => el.addEventListener('click', (e) => clickRoom(e, el)));

$('btn-clear').onclick = () => { sel.clear(); lastClicked = null; paint(); };
$('date').onchange = refresh;
$('btn-today').onclick = () => {
  $('date').value = new Date().toLocaleDateString('sv');
  refresh();
};
$('btn-print').onclick = () => {
  window.open('/print?date=' + $('date').value, '_blank');
};

$('btn-undo').onclick = async () => {
  try { await post('/api/undo'); refresh(); } catch (e) { showErr(e.message); }
};

// --- Check In ---
$('btn-in').onclick = () => {
  $('in-rooms').textContent = sel.size + ' room(s): ' + [...sel].sort().join(', ');
  $('in-arr').value = $('date').value;
  const next = new Date($('date').value);
  next.setDate(next.getDate() + 1);
  $('in-dep').value = next.toLocaleDateString('sv');
  $('dlg-in').showModal();
};
$('in-cancel').onclick = () => $('dlg-in').close();
$('in-ok').onclick = async () => {
  try {
    await post('/api/checkin', {
      rooms: [...sel],
      category: $('in-cat').value,
      arrival: $('in-arr').value,
      departure: $('in-dep').value,
    });
    $('dlg-in').close();
    refresh();
  } catch (e) {
    $('dlg-in').close();
    showErr(e.message);
  }
};

// --- Check Out ---
// Normal departures already carry a date from check-in; this is the early
// departure path. Destructive beyond a handful of rooms, so it confirms.
$('btn-out').onclick = async () => {
  const list = [...sel].sort();
  if (list.length > 3 && !confirm(
    `Check out ${list.length} rooms on ${shownDate()}?\n\n${list.join(', ')}`)) return;
  try {
    await post('/api/checkout', { rooms: list, date: $('date').value });
    refresh();
  } catch (e) { showErr(e.message); }
};

// --- Burger menu ---
const menuPop = $('menu-pop');
const menuBtn = $('btn-menu');

function setMenu(open) {
  menuPop.hidden = !open;
  menuBtn.setAttribute('aria-expanded', String(open));
}

menuBtn.onclick = (e) => {
  e.stopPropagation();
  setMenu(menuPop.hidden);
};
// Clicking anywhere else closes it, but not a click inside the menu itself —
// flipping the toggle should not dismiss the thing you are reading.
document.addEventListener('click', (e) => {
  if (!menuPop.hidden && !menuPop.contains(e.target)) setMenu(false);
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') setMenu(false);
});

$('opt-kc').onchange = async (e) => {
  const on = e.target.checked;
  document.body.classList.toggle('kc-track', on);
  try {
    await post('/api/settings', { keycard_tracking: on });
  } catch (err) {
    e.target.checked = !on;
    document.body.classList.toggle('kc-track', !on);
    showErr(err.message);
  }
};

// --- Keycards ---
// Double-click ticks a card off. Single clicks are swallowed so that aiming at
// the badge never selects the room underneath by accident.
document.querySelectorAll('.kc').forEach((el) => {
  el.addEventListener('click', (e) => e.stopPropagation());
  el.addEventListener('dblclick', async (e) => {
    e.stopPropagation();
    if (!document.body.classList.contains('kc-track')) return;
    try {
      const { baked, icon } = await post('/api/keycard', {
        date: $('date').value,
        room: el.dataset.room,
      });
      el.classList.toggle('kc-done', baked);
      // The badge is two different pictures under a custom icon set, and the
      // server is the one that knows which. Markup comes from IconSVG.
      if (icon) el.innerHTML = icon;
    } catch (err) { showErr(err.message); }
  });
});

// Dates are rendered server-side too — the board, the totals line and the
// stays panel all carry them — so this reloads rather than repainting.
$('opt-months').onchange = async (e) => {
  const on = e.target.checked;
  try {
    await post('/api/settings', { month_names: on });
    location.reload();
  } catch (err) {
    e.target.checked = !on;
    showErr(err.message);
  }
};

// Icons are rendered server-side, so this one reloads rather than repainting.
$('opt-icons').onchange = async (e) => {
  const on = e.target.checked;
  try {
    await post('/api/settings', { custom_icons: on });
    location.reload();
  } catch (err) {
    e.target.checked = !on;
    showErr(err.message);
  }
};

paint();
