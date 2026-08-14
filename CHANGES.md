# Changes — session of 2026-08-12

Handoff for the work done on top of the delivered v1. Read
[CLAUDE.md](CLAUDE.md) first for the invariants; this file records what moved
and why.

---

## Summary

Three things happened, in this order:

1. The delivered source was unpacked into a git repository and pushed.
2. A CI/CD pipeline was built and proven green before any feature work.
3. Two features landed: **keycards** and **uploadable icon sets**, along with a
   redesign of the board.

Every change was compiled, tested and rendered in CI. Nothing in this session
was built on this machine — there is no Go toolchain here, which is why the
pipeline came first.

## Upgrading

**No data migration.** Every new field is additive and decodes correctly from a
file written by the previous build. Drop the new `cleanlist.exe` in beside the
existing `cleanlist-data.json` and everything carries over.

This was verified rather than assumed: the new binary was started against a data
file written by the old one, holding 33 stays and 10 loans and having no
`settings`, `baked` or icon keys at all. All 33 stays and 10 loans survived,
`settings` was seeded to its defaults, and the upgraded shape was written
straight back.

Copy the data file aside first anyway. It is still the only copy of live
occupancy apart from the daily backups.

---

## Features

### Keycards

A card has to be encoded for every arriving guest, so the list is every room
resolving to `AF` or `AN / AF`. `NeedsKeycard` in `keycards.go` is the only
place that decision is made.

- **The list is derived, never stored.** It reads the same stays the cleaning
  board reads, so it cannot drift out of step with the sheet printed beside it,
  and a stay edited after printing produces a corrected list next time rather
  than a stale one.
- **The printed sheet leads the run and prints once**, unlike the floor sheets
  which print twice. Encoding cards is a reception job done at the desk, so
  there is nobody to hand a second copy to. **Print is now 8 pages, not 7.**
- **Double-clicking a card on the board marks it made**, red to blue. This is a
  second pass over a sheet that has already been crossed off by hand — the paper
  is still the record. It is switchable from the burger menu.

Two decisions worth keeping:

- **The tick goes through `MutateNoUndo`, not `Mutate`.** Undo holds exactly one
  step and exists to protect stay data; letting a keycard tick spend it would
  have quietly disarmed the net between a mis-selected Check Out and losing the
  stays. Any future bookkeeping gesture belongs on the same path.
- **`Settings` is a pointer on `State`** so a file written before settings
  existed is distinguishable from one where reception deliberately switched
  things off. As a plain struct, the check-off would have defaulted *off* on
  every existing installation purely because `bool` starts `false`.

Ticks prune after 90 days — they are a same-day double-check, and keeping a
season of them would grow the data file for nothing.

### Uploadable icon sets

The drawings in `icons.go` remain the default. A hotel can upload its own PNGs
from `/icons` and switch between the two sets from the burger menu. Board,
collection sheet and printed sheets all follow the switch together, because they
all render through `IconSVG`.

- **Uploads live in an `icons/` directory beside the executable, not in
  `cleanlist-data.json`.** That file is the only copy of live occupancy, a
  corrupt one deliberately stops the app starting, and thirty daily backups
  would carry every uploaded picture forever. Out in its own directory a missing
  or broken image falls back to the drawing and costs nobody a booking.
- **`IconSlots()` is a fixed allowlist.** A file is written under the slot it
  was uploaded to, never under the name the browser sent, and `handleIconFile`
  rebuilds the path from that same list. There is no traversal to defend
  against rather than one that is merely filtered.
- **PNG is checked by magic bytes, not extension**, size is capped at 512KB, and
  the write is temp-then-rename like the data file.
- **`iconMode` caches the mode and file list** so `IconSVG` never reaches back
  into the `Store`. Templates render *after* the store lock is released, and a
  helper that took it again would deadlock the first time someone moved a render
  call inside a `Read`.

`sample-icons/` holds plain black squares for testing the switch end to end.
Note that `keycardred.png` and `keycardblue.png` are both black squares, so with
a custom set active the made/unmade states look identical until real artwork
replaces them — only the badge border still changes colour.

The safe key's slot is **`key`**, not `safekey`; `safekey` is the item id. The
icons page lists each slot's exact filename.

### Board redesign

- Columns capped at 1080px and centred. They previously stretched to the window,
  which on a wide monitor left a room number and its marker at opposite ends of
  the screen.
- Rows are taller, and striped grey-blue on grey instead of ruled on white. This
  is read all day under strip lighting.
- **Items out to a room now show on the room itself**, overdue first, instead of
  behind a tab reception had to remember to open.
- Links moved into a burger menu top-right, which is also where both toggles
  live.

---

## Infrastructure

### CI — `.github/workflows/ci.yml`

Runs on push and PR. `gofmt`, a guard that `go.mod` has grown no dependencies,
`go vet`, the full suite, then a print job that renders `/print` and
`/inventory/print` through weasyprint and asserts the page counts.

Two guards are worth knowing about:

- **The DST test is checked for having actually run.** `TestDSTBoundaries` skips
  itself when tzdata is missing, and a skip is indistinguishable from a pass in
  the summary. It is the guard standing between the hotel and every service
  interval shifting by a day twice a year, so a silent skip is a hard failure.
- **Print output is rendered, not read.** Invariant 3 exists because a
  `colgroup` bug shipped once and was invisible in review. `ci/verify-print.py`
  seeds enough occupancy for every marker to appear, renders, and asserts 8
  pages and 1. The PDFs upload as artifacts for measuring by eye.

### Release — `.github/workflows/release.yml`

Fires on a `v*` tag. Re-runs the suite, verifies print, builds both binaries,
and publishes them with `SHA256SUMS.txt` and the rendered sheets. This replaces
carrying a USB stick — email providers block `.exe` attachments even inside a
zip, but a release download link does not have that problem.

```sh
git tag -a v1.2.0 -m "..."
git push origin v1.2.0
```

Binaries are also uploaded as artifacts on every CI run, so a build can be
pulled from any green run without tagging.

---

## Still outstanding

**The Collections rework.** Requested but not built: the inventory view should
become a grouped item list where items can be added and removed, each line
showing its room and date, rather than redrawing the whole 59-room grid once per
per-room item. The board half of that request is done — room notices now appear
on the cleaning board — but `/inventory` itself is unchanged.

## Notes for whoever picks this up

- **There is no Go toolchain on this machine.** All builds go through CI. A
  compile error therefore costs a full round trip, so it is worth being careful
  rather than iterating.
- The repository is **public**. The data file is gitignored, as is the runtime
  `icons/` directory.
- The GitHub PAT used to push this session is sitting in plaintext at
  `../claude-checklist.md`, outside the repo. **Rotate it.**
- Print layout is still tuned empirically. Weasyprint catches page counts and
  gross column errors; the hotel's actual printer has the final say.

---

# Changes — session of 2026-08-14 — v1.2.0

Second session on top of v1.1.0. Read [CLAUDE.md](CLAUDE.md) for the invariants;
this records what moved and why.

## Summary

Four things, in the order they were asked for:

1. **The Stays panel reads current-first**, in bold, instead of oldest-first.
2. **Dates are written the way the hotel writes them** — `14-08-26`, or
   `14-Αύγ-26` with a new menu option.
3. **Payment tracking**: unpaid rooms glow red on the board, a Paid button on
   the toolbar, and an unpaid list on the collection printout.
4. **The collection sheet lost its blank rows** so both its tables share a page.

Everything was compiled and tested in CI. The print layout was additionally
rendered locally through **Chrome** rather than weasyprint — the same engine the
reception PC prints with — and measured rather than eyeballed.

## Upgrading

**Read this before dropping the binary on the reception PC.**

There is no migration and nothing serialised changed shape. `Stay.Paid` and
`Settings.MonthNames` are both additive and both decode correctly from a file
written by v1.1.0. Drop the new `cleanlist.exe` in beside the existing
`cleanlist-data.json` and every stay, loan and setting carries over.

One visible consequence, chosen deliberately:

**Every existing stay reads as unpaid.** Unpaid is the zero value, and it is the
only honest default — a stay written by the previous build is one nobody has
confirmed payment for. So on the first run the board shows a red outline on
every room with a guest in it, and the collection sheet lists them all.
Reception clears it by selecting the rooms that have paid and pressing Paid
once. A few minutes on the first day, accurate from then on.

The alternative — a migration marking every existing stay paid — asserts money
was received when nothing in the system knows whether it was, and wrong data
must never look like normal data. If the hotel would rather start from a clean
slate anyway, it is a five-line migration, but it should be a decision someone
makes out loud rather than something that happens quietly.

Copy the data file aside first regardless. It is still the only copy of live
occupancy apart from the daily backups.

---

## Stays panel reads current-first

Selecting a single room opens the Stays panel under the board. It listed a
room's stays oldest first, so on a room with any history the guest actually in
there was at the bottom. It now opens with the stay covering the date on screen,
in **bold**, then everything else newest-arrival-first.

- **Not a plain reverse sort.** Newest-first alone puts a booking taken for next
  month above the guest currently in the room, which is the opposite of what the
  panel is read for. The covering stay is pinned to the top and the rest follow.
- **On a turnover day both stays cover the date**, so both are bold, incoming
  on top.
- **It follows the date picker, not the wall clock.** Move the board to next
  week and the panel bolds whoever is in the room *then*.

`CoversDate` in `model.go` is the one definition of "covering", factored out of
`CurrentStay` so the panel and the Check Out action cannot drift apart. It uses
`DayNum`, per invariant 1. Ordering happens in `RoomStays` in Go, not in
`board.js`; `/api/room` takes a `date` and returns a `current` flag per stay.

**The stored order is untouched** — `State.Stays` stays ascending by arrival,
which is what the overlap check and `Resolve` read. `TestInsertionOrderIrrelevant`
still pins that.

## Dates

Two formats now, and they must not be confused:

| | |
|---|---|
| `FormatDate` → `2026-08-14` | ISO. Storage, the API, every `<input type="date">`. |
| `FormatGreek` → `14-08-26` | Display only. The single place a date is formatted for a person. |

Previously the display format was `14/08/2026` and the Stays panel showed the
raw stored `2026-08-14`, so there were two visible formats and neither matched
how the hotel writes a date by hand.

**A menu option writes the month out**: `14-Αύγ-26`, on screen and on the
printed sheets alike. Greek months, because everything else on the sheet is —
Ιαν, Φεβ, Μάρ, Απρ, Μάι, Ιούν, Ιούλ, Αύγ, Σεπ, Οκτ, Νοέ, Δεκ. June and July
carry four letters so they cannot be taken for one another. Off is the default
and the zero value, so an existing file keeps its numeric dates.

`dateMode` caches the setting rather than reading the Store, for the same reason
`iconMode` does: templates render after the lock is released and a formatter
that took it again would deadlock.

**The date picker itself is not ours to format.** `<input type="date">` renders
in the browser's locale and no CSS or markup overrides it. Same class of problem
as the browser's print headers — do not try to fix it in code.

## Payment tracking

`Stay.Paid` records that reception has taken the money. It hangs off the **stay,
not the room**: a room is not paid, a guest is, and per-room the next arrival
inherits the last guest's settled bill.

**On the board**, a room with a guest in it whose stay is unpaid gets a red
outline, a halo, and a red dot by the room number for anyone who cannot pick the
colour out of the stripes. Vacant rooms never glow. Deliberately not animated:
this sits on a reception screen all day, and a board that pulses is a board
people stop looking at. A 3px gap keeps a run of unpaid rooms reading as several
rooms rather than one red block.

**A Paid button** sits between Check Out and Clear. It lights up only for
selected rooms that actually have a guest, and always confirms — "Confirm room
210 has been paid in full." — however few rooms are selected, because money is
the one thing on this board nobody can verify by looking at the room.

Two things past the literal request, both because the alternative loses money:

- **The button reverses.** Once everything selected is settled it reads "Not
  paid" and asks to unmark. A one-way flag means one mis-click quietly writes
  off a real debt.
- **A selection containing a vacant room is refused whole**, naming the rooms,
  rather than marking some and skipping the rest silently.

`UnpaidOn` asks whether **any** covering stay is unpaid, so on a turnover day
either guest owing shows and a debt cannot hide behind the other's settled bill.
`SetPaid` marks every covering stay, so one press settles the day.

`/api/paid` runs on `MutateNoUndo`, like the keycard tick: undo holds one step,
it protects stay data from a mis-selected Check Out, and a bookkeeping gesture
must not spend it.

## Collection sheet

The printout gained an **unpaid rooms** table — room, category, departure date,
in printed room order. Departure is what makes it actionable: a guest leaving
tomorrow who has not paid is a different problem from one staying another week.

**Neither table on this sheet is padded**, and that took two passes to get
right. Blank ruled rows are writing space for a housekeeper walking a corridor,
which is why the cleaning sheets and the keycard list keep theirs. Here they
were rows for items nobody has out and rooms that owe nothing — and a first
table padded to 24 rows pushes the second onto a page of its own however short
both lists are.

`.sheet-join` overrides `break-before` back to `auto`, so the unpaid table rides
under the collection list. The two share a page and spill onto a second only
when genuinely long; `thead` is a header group, so a table that splits reprints
its headings.

Measured on a Chrome render of a seeded mid-August day — 4 items out, 10 rooms
owing: one page. Twenty item rows moves the unpaid table over; twelve still
shares.

## Infrastructure

- **`ci/verify-print.py` renders more.** Both sheets a second time with month
  names on, since that is the wider date format and the collection sheet's due
  column is a fixed 3.6cm. The unpaid page is checked in both states — rooms
  owing, and nothing owed — and `EXPECT_COLLECT_PAGES` guards against padding
  creeping back and growing a second page.
- **`ci/__pycache__` is untracked**, having been committed by accident with the
  pipeline and rewritten on every run.

## Still outstanding

- **The Collections rework**, unchanged from the last session: `/inventory`
  should become a grouped item list rather than redrawing the 59-room grid once
  per per-room item.
- **The unpaid list only covers current stays.** A guest who left owing money
  does not appear. That matches "rooms that are currently unpaid" as asked for,
  and keeps the list bounded, but it is not an accounts-receivable report.
- **The PAT is still in plaintext** at `../claude-checklist.md` and has now been
  used across two sessions. Rotate it.
- Print layout is still tuned empirically. Chrome renders locally now, which is
  a closer match than weasyprint, but the hotel's printer has the final say.
