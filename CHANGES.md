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

# Changes — session of 2026-08-14

## Stays panel: current booking first, in bold

Selecting a single room opens the Stays panel under the board. It listed a
room's stays oldest first, so on a room with any history the guest actually in
there was at the bottom of the list. It now reads top-down:

1. Any stay covering the date the board is showing, in **bold**.
2. Everything else, newest arrival first.

Two details worth knowing:

- **It is not a plain reverse sort.** A pure newest-first order puts a booking
  taken for next month above the guest currently in the room, which is the
  opposite of what the panel is read for. The covering stay is pinned to the
  top and the rest run newest-first below it.
- **On a turnover day both stays are current** and both are bold — the arrival
  and departure days each count as covered, same as everywhere else — with the
  incoming stay on top.

The panel follows the date picker, not the wall clock: move the board to a date
next week and the panel bolds whoever is in the room *then*.

`CoversDate` in `model.go` is the one definition of "covering", factored out of
`CurrentStay` so the panel and the Check Out action cannot drift apart. It uses
`DayNum`, per invariant 1.

Ordering happens in Go (`RoomStays` in `main.go`), not in `board.js`. `/api/room`
now takes a `date` parameter and returns a `current` flag per stay; the page just
paints `tr.now` and the CSS bolds it.

**The stored order is unchanged** — `State.Stays` stays sorted ascending by
arrival, which is what the overlap check and `Resolve` read.
`TestInsertionOrderIrrelevant` still pins that, and `roomstays_test.go` covers
the new display order.

**No data migration.** Nothing serialised changed shape; `roomStay` is a view
type that exists only in the API response.

## Dates read DD-MM-YY

Every date shown to a person is now written `14-08-26`. Previously the display
format was `14/08/2026` and the Stays panel showed the raw stored `2026-08-14`,
so the app had two visible formats and neither matched how the hotel writes a
date by hand.

`FormatGreek` in `rules.go` is the single place this is decided — the board, the
totals line, the printed sheets, the collection sheet, the inventory list and
every due date all render through it.

**Display only. Nothing stored changed.** Dates are still held, sent to the API
and put into `<input type="date">` as ISO `YYYY-MM-DD` through `FormatDate`, and
that cannot change: the date picker refuses any other format and the day
arithmetic parses it. `/api/room` now returns both — `arrival_gr` to show,
`arrival` to send back when deleting a stay — and the sort still runs on the ISO
field, where lexical order is chronological order.

The two `confirm()` dialogs on the board were showing ISO dates at people and
now show the display format as well.

**The date picker itself is not ours to format.** `<input type="date">` renders
in the browser's own locale — on a Greek Windows box it reads `14/08/2026` — and
no CSS or markup can override it. Same class of problem as the browser's print
headers.

`TestDisplayDateFormat` pins both formats, including the leading zeros that keep
the printed due-date column an even width.

## Month names, as a menu option

The burger menu gained **Month names in dates**. Off, dates read `14-08-26`;
on, they read `14-Αύγ-26` — on screen and on the printed sheets, since both go
through `FormatGreek`.

Months are Greek because everything else on the sheet is: Ιαν, Φεβ, Μάρ, Απρ,
Μάι, Ιούν, Ιούλ, Αύγ, Σεπ, Οκτ, Νοέ, Δεκ. June and July carry four letters so
they cannot be mistaken for each other.

`Settings.MonthNames` is additive and **off is the zero value**, so a data file
written by any previous build keeps the numeric dates it already had. No
migration.

`dateMode` in `rules.go` caches the setting rather than reading the Store, for
the same reason `iconMode` does: templates render after the Store lock is
released and a formatter that took it again would deadlock. `RefreshDateFormat`
is called at startup and after a settings change.

The JS date formatter added in the previous change is gone. The board's date
picker now carries its own rendered date in `data-gr`, so the Check Out confirm
quotes the server's formatting instead of duplicating it — one formatter, per
invariant 5.

**`ci/verify-print.py` now renders both sheets twice**, once per date format.
Month names are the wider of the two — an overdue collection line reads
`26-Ιούλ-26 (+6)` against a fixed 3.6cm due column — so the mode is verified by
rendering rather than by counting characters. The extra PDFs land in the
`printed-sheets-pdf` artifact as `print-months-*.pdf` and
`collection-months-*.pdf`.

## Payment tracking

Three parts, all driven off one new field.

**`Stay.Paid`.** Payment belongs to the stay, not the room — hang it off the
room and the next arrival inherits the last guest's settled bill. Additive, and
unpaid is the zero value.

**Red on the board.** A room with a guest in it whose stay is unpaid gets a red
outline and a halo, plus a small red dot by the room number for anyone who
cannot pick the colour out of the stripes. Vacant rooms never glow. It is
deliberately not animated: this sits on a reception screen all day, and a board
that pulses is a board people stop looking at.

**A Paid button**, between Check Out and Clear. It lights up only for selected
rooms that actually have a guest, and always confirms — "Confirm room 210 has
been paid in full." — however few rooms are selected, because money is the one
thing on this board nobody can verify by looking at the room.

Two things beyond the literal request, both because the alternative loses money:

- **The button reverses.** When everything selected is already settled it reads
  "Not paid" and asks "Mark room 210 as NOT paid?". A one-way flag means a
  mis-click quietly writes off a real debt with no way back.
- **A selection containing a vacant room is refused whole**, naming the rooms,
  rather than marking the occupied ones and silently skipping the rest.

**An unpaid page on the collection printout**, listing room, category and
departure date in printed room order. Departure is what makes it actionable: a
guest leaving tomorrow who has not paid is a different problem from one staying
another week.

## Upgrading to this build

**Read this one.** There is no data migration and nothing changes shape, but
there is a visible consequence the first morning.

Every stay in the existing data file decodes as **unpaid**, because that is the
zero value and nobody has said otherwise. So on first run the board shows a red
outline on every room with a guest in it, and the collection sheet prints an
unpaid page listing them all. Reception clears it by selecting the rooms that
have paid and pressing Paid once — it is a few minutes on the first day and
accurate from then on.

This was a choice, not an oversight. The alternative is a migration marking
every existing stay paid, which asserts money was received when nothing in the
system knows whether it was. Wrong data must never look like normal data. If the
hotel would rather start from a clean slate anyway, say so and it is a five-line
migration — but it should be a decision someone makes out loud.

**The collection sheet is now two pages when anything is owed** and one page when
nothing is. `ci/verify-print.py` checks both states.
