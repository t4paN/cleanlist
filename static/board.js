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

async function loadDetail(room) {
  const r = await fetch('/api/room?room=' + encodeURIComponent(room));
  const { stays, labels } = await r.json();
  $('detail-room').textContent = room;
  const body = $('detail-body');
  body.innerHTML = '';
  if (!stays.length) {
    body.innerHTML = '<tr><td colspan="4" style="color:var(--dim)">No stays recorded.</td></tr>';
  }
  stays.forEach((s) => {
    const tr = document.createElement('tr');
    [labels[s.category] || s.category, s.arrival, s.departure].forEach((v) => {
      const td = document.createElement('td');
      td.textContent = v;
      tr.appendChild(td);
    });
    const td = document.createElement('td');
    const b = document.createElement('button');
    b.className = 'danger';
    b.textContent = 'Remove';
    b.onclick = async () => {
      if (!confirm(`Remove the stay ${s.arrival} → ${s.departure} in room ${room}?`)) return;
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
    `Check out ${list.length} rooms on ${$('date').value}?\n\n${list.join(', ')}`)) return;
  try {
    await post('/api/checkout', { rooms: list, date: $('date').value });
    refresh();
  } catch (e) { showErr(e.message); }
};

paint();
