# Cleanlist — Housekeeping Sheet Generator

Spec for implementation. Target: single Windows binary, cross-compiled from Linux Mint.

---

## 1. Purpose

Replaces a manual two-step paper workflow at hotel reception:

**Current:** print a blank room grid → reception fills it in by hand from memory of
each guest's service rules → reception re-copies the columns onto separate
per-section sheets → print twice → housekeepers split the floors between themselves.

**New:** reception maintains room occupancy on screen → presses Print → the
per-section sheets come out already filled in, twice each.

The transcription step disappears. The "which rooms get towels today" recall
disappears. Nothing about how housekeeping receives or splits the paper changes.

---

## 2. Stack and constraints

- **Language: Go.** Non-negotiable driver is `GOOS=windows GOARCH=amd64 go build`
  producing one static `.exe` from the dev machine (Linux Mint 21.3). No runtime,
  no installer, copy to the reception PC on a USB stick.
- **UI: local web server.** Binary listens on `127.0.0.1:8080`, serves the pages,
  opens the default browser on launch. Avoids cgo and native GUI toolkits entirely.
- **No cgo anywhere.** This rules out `mattn/go-sqlite3`. See §5.
- **Assets embedded** via `//go:embed`. Deployment must be exactly one file.
- **Stdlib only.** `net/http`, `html/template`, `embed`, `encoding/json`, `time`.
  No framework, no build step, no CDN on the frontend — plain HTML/CSS/JS.

### Known deployment friction
First launch on Win11 shows a SmartScreen "unrecognised app" warning (unsigned
binary) and likely a Windows Firewall prompt. Both are one-time click-throughs.
Bind to `127.0.0.1` rather than `0.0.0.0` — this often avoids the firewall prompt
and there is no reason to expose the port.

---

## 3. Domain model

### 3.1 Rooms (static)

Three sections, matching the existing paper sheets exactly:

| Section key | Label | Rooms |
|---|---|---|
| `ground` | Ισόγειο / Παράρτημα | 100, 101, 102, 103, 104, 105, 106, 400, 401, A01 |
| `floor2` | 2ος Όροφος | 201–224 |
| `floor3` | 3ος Όροφος | 301–324 |

Defined as a Go slice in source. Room IDs are strings, not ints (`A01` exists).
Order within a section is the print order.

### 3.2 Markers

| Code | Meaning |
|---|---|
| `AF` | άφιξη — arrival / check-in |
| `AN` | αναχώρηση — departure / check-out |
| `F` | freshen up |
| `S` | sheets |
| `S & P` | sheets and towels |
| `---` | vacant |

`AN / AF` in a single cell for same-day turnover (see §6.3).

Marker strings live in one config block. Note that `AF`/`AN` are Greek-derived
but `S`/`P` map to σεντόνια/πετσέτες — pick one alphabet for the source literals
and keep it consistent so string comparison never surprises anyone.

### 3.3 Stays

**A room holds a list of stays, not a single occupancy record.** This is required
by same-day turnover: one room can have an outgoing stay ending and an incoming
stay starting on the same date.

```json
{
  "204": [
    { "id": "a1b2", "category": "booking", "arrival": "2026-07-20", "departure": "2026-07-24" },
    { "id": "c3d4", "category": "group-acme", "arrival": "2026-07-24", "departure": "2026-07-27" }
  ]
}
```

Validation: stays in a room must not overlap. Sharing a single date, where one
stay's `departure` equals the next stay's `arrival`, is the one permitted
adjacency. Reject anything else with a clear message naming both stays.

### 3.4 Category profiles

**Categories are editable data, not hardcoded.** Travel agency groups arrive with
their own contract terms, so each category carries its own rules.

```json
{
  "id": "booking",
  "label": "Booking.com",
  "long_stay": { "first_change_day": 3, "interval": 2, "marker": "S & P" },
  "short_stay": {
    "2": {},
    "3": { "3": "S" },
    "4": { "3": "S & P" }
  }
}
```

- `short_stay` — keyed by **total nights**, then by **day of stay**. Any nights
  value present here is fully governed by its table; days not listed default to
  `F`. A stay of 2 nights with an empty table therefore gets `F` throughout,
  which is the intended behaviour.
- `long_stay` — applies to any stay whose total nights is **not** a key in
  `short_stay`. First change lands on `first_change_day`, then repeats every
  `interval` days.
- Day numbering: **arrival day is day 1.** The editor in §6.4 renders this
  visually so nobody has to hold the convention in their head.

Ship with three seeded profiles — Booking.com, Group, Other — matching the rules
currently in use. They are starting points, freely editable and deletable.

A category in use by an existing stay cannot be deleted. Offer to reassign those
stays instead.

---

## 4. Rules engine

For a room and a target date `D`, resolve the printed cell.

### 4.1 Resolution order

1. Collect **event markers**: `AN` if any stay in the room has `departure == D`;
   `AF` if any stay has `arrival == D`.
2. If both, the cell is `AN / AF`. If one, that marker alone.
3. If neither and no stay covers `D`, the cell is `---`.
4. Otherwise a stay covers `D` mid-way. Resolve its **service marker**:
   - `nights = departure - arrival`
   - `dayOfStay = D - arrival + 1`
   - If `nights` is a key in the category's `short_stay`, look up `dayOfStay`.
     Found → that marker. Not found → `F`.
   - Else, if `dayOfStay >= first_change_day` and
     `(dayOfStay - first_change_day) % interval == 0` → `long_stay.marker`.
     Otherwise `F`.

Event markers win over service markers. A departing room is being stripped
regardless; an arriving room was made up before the guest walked in.

### 4.2 Date handling

Use `time.Time` truncated to date, local timezone, and compute day differences on
the **date components** — not by dividing a `Duration` by 24h. Greece observes DST,
and a 23-hour day will silently produce an off-by-one on the interval twice a
year. **This is the single most likely correctness bug in the app.** Write a unit
test that spans the March and October DST boundaries.

---

## 5. Persistence

**Single JSON file, `cleanlist-data.json`, next to the executable.** Holds rooms'
stays and the category profiles.

Not SQLite. The Go SQLite driver needs cgo, which breaks the clean cross-compile
from Mint, and the dataset is ~58 rooms. JSON is also hand-inspectable and
hand-fixable in Notepad if something goes wrong at 7am, which on a reception PC
matters more than query performance.

### Write safety — implement this properly, do not skip

This file is the only copy of live occupancy data on a machine that gets switched
off abruptly.

1. **Atomic writes.** Marshal to `cleanlist-data.json.tmp`, `f.Sync()`, close,
   then `os.Rename` over the real file. Never truncate-and-write in place.
2. **Daily backup.** On the first write of each calendar day, copy the existing
   file to `backups/cleanlist-YYYY-MM-DD.json` first. Keep 30, delete older.
3. **Load failure is loud.** If the JSON is missing or corrupt on startup, do not
   silently start with an empty board — that looks like every room is vacant and
   reception might not notice until housekeeping is already out. Show an error
   page naming the backups directory.

Write on every mutation. There is no save button.

---

## 6. Interface

### 6.1 Board

All rooms in three side-by-side columns mirroring the existing master grid:
ground/annex, floor 2, floor 3. Each cell shows room number, today's resolved
marker, and the category label if occupied.

Colour or weight distinguishes vacant / F / S / S & P / AF / AN at a glance. Keep
it legible on a reception PC that is probably a small 1366×768 screen.

### 6.2 Selection

Click toggles a room. Shift-click selects a range within a column. Visible
selection count and a clear-selection control.

### 6.3 Actions

Operating on the current selection:

- **Check In** — dialog: category (dropdown of profiles), arrival date (default
  today), departure date. Creates a stay on every selected room. Rejects overlaps.
- **Check Out** — sets the current stay's `departure` to today. This is the
  early-departure path; normal departures already have a date from check-in and
  need no action.
- **Edit / Remove** — available when exactly one room is selected. Lists that
  room's stays for amendment or deletion.

Because departure dates are captured at check-in, `AN` resolves without anyone
pressing anything on the day. Sheets can be printed at 8am for a room that
checks out at 11am and still be correct.

### 6.4 Category editor

Reception staff edit these, so it must be a proper form — no raw JSON.

**Do not present day numbers abstractly.** For each short-stay entry, render the
stay as a labelled strip of days, with fixed cells for arrival and departure and
a marker dropdown on each day between:

```
3 nights:   [Day 1: AF]  [Day 2: F ▾]  [Day 3: S ▾]  [Day 4: AN]
```

This removes the off-by-one ambiguity entirely — whoever edits it sees the actual
shape of the stay rather than reasoning about whether day 1 is the arrival.

For long stays, show a **live preview strip of days 1–14** with markers resolved
from the current `first_change_day` and `interval`, updating as the fields change.
Same reasoning: make the rule visible instead of asking anyone to simulate it.

Controls: add/remove category, add/remove a short-stay entry for a given number of
nights, edit label and long-stay fields.

### 6.5 Undo

**Required, not optional.** Check Out or Remove on a mis-selected range destroys
stay data with no way to reconstruct it. Keep the previous full state snapshot in
memory and offer a single-level "Undo last action".

Confirmation dialog on any destructive action affecting more than 3 rooms.

---

## 7. Print output

A separate print view rendered from the same data, then `window.print()`. The
standard browser print dialog is fine and expected — no kiosk flags.

### Structure

Six pages, in order:

1. Ground/annex, copy 1
2. Ground/annex, copy 2
3. Floor 2, copy 1
4. Floor 2, copy 2
5. Floor 3, copy 1
6. Floor 3, copy 2

Each section is a `<div>` with `break-before: page`. The two copies are identical
markup rendered twice — housekeepers split the floors between themselves, so do
not try to be clever and divide rooms between them.

### Page layout

Three columns, matching the existing sheet proportions:

| Column | Width | Content |
|---|---|---|
| Room | ~5.5cm | Room number |
| Marker | ~1cm | `AF` / `AN` / `F` / `S` / `S & P` / `---` |
| Notes | ~11.5cm | Empty — where the housekeeper writes |

The marker column at ~1cm is too narrow for `S & P` and `AN / AF`. Either widen
it to ~2cm and take the space from notes, or allow that column to size to content.
Do not let it wrap.

Above the table: section label and date in Greek format (DD/MM/YYYY). The existing
sheets carry no printed date at all; adding it costs nothing.

### Print CSS

```css
@page { size: A4 portrait; margin: 15mm; }
@media print {
  .no-print { display: none; }
  thead { display: table-header-group; }
  tr { break-inside: avoid; }
  .section { break-before: page; }
  .section:first-child { break-before: auto; }
}
```

Row height must leave real writing space in the notes column — target the density
of the current sheet, roughly 24 rows filling a page comfortably.

### Target date

A date field next to Print, defaulting to today, so reception can print
tomorrow's sheets the evening before. All rules evaluate against this date rather
than `time.Now()`.

---

## 8. Out of scope for v1

- ODT generation. Print is the only output.
- Guest names, booking references, rates, PMS integration.
- Multi-user, network access, authentication.
- Service history or reporting.
- Per-room exceptions (DND, guest refused). Housekeepers write those on the paper
  today and can keep doing so.

---

## 9. Acceptance

**Build and deploy**
- [ ] `GOOS=windows GOARCH=amd64 go build` from Mint produces a single `.exe`
      that runs on Win11 with nothing installed.
- [ ] Launching opens the browser to the board automatically.

**Rules**
- [ ] 2-night Booking.com stay: `AF`, `F`, `AN`.
- [ ] 3-night: `AF`, `F`, `S`, `AN`.
- [ ] 4-night: `AF`, `F`, `S & P`, `F`, `AN`.
- [ ] 10-night with `first_change_day: 3`, `interval: 2`: `S & P` on days 3, 5, 7, 9;
      `F` elsewhere; `AF` day 1, `AN` day 11.
- [ ] A category with different short-stay rules resolves independently of the
      others on the same date.
- [ ] Same-day turnover renders `AN / AF` in one cell.
- [ ] Vacant room renders `---`.
- [ ] DST test: a stay spanning the March and October transitions produces the
      same day numbering as one that does not.

**Editing**
- [ ] Category editor round-trips a new profile without touching the JSON by hand.
- [ ] Long-stay preview strip updates live as interval fields change.
- [ ] Deleting a category in use is blocked with a reassignment offer.
- [ ] Overlapping stays are rejected; departure-equals-arrival is accepted.
- [ ] Undo restores state after a multi-room Check Out.

**Print and durability**
- [ ] Print produces exactly 6 pages, correct sections and order, no UI controls
      visible, notes column empty and wide enough to write in.
- [ ] `S & P` and `AN / AF` fit the marker column without wrapping.
- [ ] Kill the process mid-session, relaunch: all state intact.
- [ ] Delete the data file, relaunch: clear error naming the backups directory,
      not a silently empty board.
