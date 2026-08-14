# CLAUDE.md

Guidance for Claude Code working in this repository.

## What this is

Cleanlist generates the daily housekeeping sheets for a Greek island hotel. It
replaces a two-step paper workflow: reception used to print a blank room grid,
fill it in from memory of each guest's service interval, then hand-copy the
columns onto separate per-floor sheets. Now reception records stays on screen
and presses Print.

**It runs on a reception PC in a working hotel.** That shapes every decision
below. It has to start reliably on a Windows box nobody administers, survive
being switched off at the wall, and never quietly show wrong data. Cleverness
that risks any of those is not worth it.

## Stack

Go, standard library only. No third-party modules, no frontend framework, no
build step, no CDN. HTML/CSS/JS are plain and embedded in the binary.

```
main.go             HTTP server, routes, API handlers, page data
model.go            rooms, stays, categories, settings, Store (persistence)
rules.go            date arithmetic and marker resolution
keycards.go         which rooms need a card encoded, derived from arrivals
inventory.go        loanable items, loans, collections, room notices
icons.go            inline SVG icon set
rules_test.go       rules coverage including the DST guard
static/             embedded assets — templates, CSS, JS
```

## Commands

```sh
go test ./...                                    # always before claiming done
go vet ./...
go build -o cleanlist .                          # Linux, for local testing
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o cleanlist.exe .
```

Cross-compiling to Windows from Linux works with no extra tooling. Keep it that
way — anything requiring cgo breaks it.

## Hard invariants

Do not violate these without an explicit discussion. Each exists because
something broke or nearly did.

### 1. Never compute day gaps with Duration arithmetic

Use `DayNum` in `rules.go`, which builds a day ordinal from calendar
components. `end.Sub(start).Hours()/24` is wrong: Greek DST makes two days a
year 23 or 25 hours long, and the naive version shifts every service interval by
one. `TestDSTBoundaries` pins this under `Europe/Athens`. If that test starts
failing, the arithmetic regressed — do not adjust the test to match.

`time/tzdata` is imported blank in `main.go` because Windows ships no timezone
database and `LoadLocation` would fail there.

### 2. No cgo, no dependencies

`go.mod` has no requires and should stay that way. In particular, do not reach
for `mattn/go-sqlite3` — the dataset is 59 rooms and JSON is fine. See
persistence below.

### 3. Print tables need an explicit `colgroup`

`table-layout: fixed` takes column widths from the **first row**, and the first
row of every print table is a spanning title cell. Without a `<colgroup>` the
widths in CSS are silently ignored and the columns collapse to equal fractions.
This bug shipped once and was invisible in review — it only showed up on
measurement.

**Any change to print column widths must be verified on a rendered PDF, not by
reading the CSS.** See "Verifying print output" below.

### 4. Persistence must stay paranoid

- Writes are atomic: temp file → `Sync` → `Rename`. Never truncate in place.
- One backup per calendar day into `backups/`, last 30 kept.
- A missing or corrupt data file makes the app **refuse to start** and show an
  error page naming the backups directory. It must never fall back to an empty
  board: that looks like every room is vacant and might not be noticed until
  housekeeping is already out.

### 5. The rules live in Go, not in JavaScript

The category editor's preview strip fetches `/api/preview` rather than
reimplementing resolution client-side. Two copies of the rules would eventually
disagree, and the printed sheet is the one that matters. Do not "optimise" this
into local computation.

### 6. Fail visibly

`fatal()` keeps the console window open waiting for Enter. A window that flashes
and vanishes tells the person standing at reception nothing. The port walker
exists for the same reason: Windows reserves blocks of ports for Hyper-V/WSL at
boot and binding one fails with `WSAEACCES` even though nothing is listening.
The app tries a list, then asks the OS for any free port, and prints the address
it got.

## Domain rules

### Markers

| Code | Meaning |
|---|---|
| `AF` | άφιξη — arrival |
| `AN` | αναχώρηση — departure |
| `F` | freshen up |
| `P` | towels only |
| `S & P` | sheets and towels (σεντόνια & πετσέτες) |
| `AN / AF` | departure and arrival the same day |
| `---` | vacant |

Defined as constants in `model.go`. All source literals are Latin characters
even where the shorthand is Greek-derived, so string comparison never surprises.

**There is no sheets-only marker.** The hotel never changes linen without also
changing towels, but does change towels alone. An earlier build had `S` for
sheets-only; it was retired in favour of `P`. Because markers are stored as
strings inside category rules, `legacyMarkers` in `model.go` rewrites them on
load. Follow that pattern if a marker is ever renamed again — changing the
constant alone leaves old data files printing a marker that no longer exists.

### Resolution order (`Resolve` in rules.go)

1. Departure on this date → `AN`. Arrival on this date → `AF`. Both → `AN / AF`.
2. No stay covering the date → `---`.
3. Otherwise a mid-stay day, resolved by `ServiceMarker`.

Event markers beat service markers. A departing room is stripped regardless; an
arriving room was made up before the guest walked in.

### ServiceMarker

Day 1 is the **arrival day**.

- If the stay's total nights is a key in the category's `short_stay` table, that
  table governs completely: a listed day gives its marker, an unlisted day gives
  `F`, and the long-stay interval is **not** consulted.
- Otherwise the repeating interval applies: first change on `first_change_day`,
  then every `interval` days.

**Last-night downgrade.** One rule sits above both paths: a full `S & P`
landing on the guest's last night is downgraded to `P`. Fresh sheets on a bed
stripped the next morning are wasted; the towels are still wanted. This applies
to explicit short-stay entries as well as to the repeating interval, so a
category configured with `S & P` on its final night will still print `P`. It is
currently hotel-wide rather than per-category — if an agency ever needs the full
change on the last night, this becomes a category flag.

Seeded categories: Booking.com changes every 2 days, Group and Other every 3.
All are editable at runtime by reception staff and are data, not code.

### Rooms

Static, in `Sections` in `model.go`. Order within a section is the print order.
Room IDs are strings — `A01` and `A02` exist. Changing the room list requires a
rebuild; that is accepted.

## Keycards

`keycards.go`. A card has to be encoded for every guest who arrives, so the list
is every room resolving to `AF` or `AN / AF` on the day — `NeedsKeycard` is the
single place that decision is made.

**The list is derived, never stored.** It reads the same stays the cleaning
board reads, so it cannot drift out of step with the sheet printed beside it,
and a stay edited after printing produces a corrected list on the next print
rather than a stale one. Do not add a keycard table to the data file.

The sheet leads the print run and prints **once**, unlike the floor sheets which
print twice. Encoding cards is a reception job done at the desk, not a
housekeeping one done in a corridor, so there is nobody to hand the second copy
to.

### The tick

`State.Baked` maps a date to the rooms whose cards have been made, and the board
double-clicks it on and off. It is a second pass for reception to confirm
against — the printed sheet, crossed off by hand, is still the record. Two
things about it matter:

- **It goes through `MutateNoUndo`, not `Mutate`.** Undo holds exactly one step
  and exists to protect stay data. Letting a keycard tick spend that step would
  quietly disarm the safety net between a mis-selected Check Out and losing the
  stays. Any future bookkeeping gesture belongs on the same path.
- **Ticks are pruned to `keepBakedDays`.** They are a same-day double-check;
  keeping a season of them grows the data file for nothing.

`Baked` is additive and decodes as nil from an older file, where nil correctly
means nothing has been ticked. No migration was needed.

### Settings

`State.Settings` is a **pointer** so that a file written before settings existed
is distinguishable from one where reception deliberately switched everything
off. Absent is seeded with `defaultSettings()` in `load`; present is left alone.
Had it been a plain struct, the keycard check-off would have defaulted off on
every existing installation purely because `bool` starts `false`. Follow that
pattern for any setting whose default is not the zero value.

## Dates

Two formats, and they must not be confused:

- **`FormatDate` → `2026-08-14`.** ISO, for storage, the API and every `<input
  type="date">`. The picker refuses anything else and `ParseDate` reads it back.
  Not negotiable.
- **`FormatGreek` → `14-08-26`, or `14-Αύγ-26`** when `Settings.MonthNames` is
  on. Display only, and the *single* place a date is formatted for a person —
  board, totals, stays panel, printed sheets, collection sheet, due dates.

Changing how dates look means changing `FormatGreek` and nothing else. If a page
needs a formatted date, render it in Go and pass it through; do not reformat in
JavaScript. `/api/room` returns both (`arrival` and `arrival_gr`) for exactly
this reason, and the board's date picker carries its own rendered value in
`data-gr` so the confirm dialogs have one to quote.

`dateMode` caches the setting and follows `iconMode`'s rule: templates render
after the Store lock is released, so the formatter must never reach back into
the Store. Call `refreshDateFormatFromStore` after anything that changes it.

**The date picker itself cannot be formatted.** `<input type="date">` renders in
the browser's locale and no CSS or markup overrides it — same class of problem
as the browser's print headers. Don't try to fix it in code.

The month-name mode is the wider format, so `ci/verify-print.py` renders both
sheets a second time with it on. The collection sheet's due column is a fixed
3.6cm and an overdue line reads `26-Ιούλ-26 (+6)`.

## Inventory

`inventory.go` holds the loanable-items domain: `Item`, `Loan`, and the
collection query. It shares the binary, the `State`, the data file and every
piece of infrastructure with the cleaning board.

The sharing is the point, not a convenience: the `checkout` return rule calls
`CurrentStay` to read the room's departure date. Splitting inventory into its
own program would mean duplicating stay data or losing that rule.

**There is no unit numbering.** An `Item` is one physical object and its number
lives in its label ("Σίδερο 2"), because that is what is painted on the iron. An
earlier build had item types with numbered unit lists; `migrateItemTypes` in
`model.go` converts that shape and is the pattern to copy for future schema
changes.

`PerRoom` items are the exception: a safe key belongs to its room and never
travels, so one `Item` stands for the whole set and the room supplies the
identity. `LendItem` therefore applies its already-out check per room for these
and globally for everything else. Getting that backwards either blocks 58 valid
safe-key loans or lets one iron go to four rooms at once.

**Icons are inline SVG in `icons.go`**, not emoji and not an icon font. Emoji
render as colour bitmaps that differ per machine and print badly; a font would
be a dependency. These are stroked paths using `currentColor`, so they inherit
text colour, stay monochrome in print and need no separate print rule beyond a
size bump. `guessIcon` assigns one from the item name — accent- and
case-insensitive for Greek — so adding an item does not force a second choice.

Other invariants here:

- **A checkout-rule loan on a room with no stay gets a blank due date**, never a
  guessed one. A blank is visible on the sheet; a wrong date is not.
- **Deleting an item is blocked while it is out**, or the loan becomes
  unreturnable.
- **`Collections` returns due-today plus overdue**, sorted into printed room
  order so collection follows the same corridor walk as cleaning.
- Open-ended loans never appear on a collection sheet by design.

`State` gained `items` and `loans` after the cleaning board shipped. Both decode
as nil from an older file, are seeded in `load`, and the upgraded shape is
written straight back so the file on disk always matches what the app holds.

## Icon sets

The drawings in `icons.go` are the default. A hotel can upload its own PNGs
from `/icons` and switch to them with `Settings.CustomIcons`.

**Uploads live in an `icons/` directory beside the executable, not in
`cleanlist-data.json`.** That placement is the point: the data file is the only
copy of live occupancy, a corrupt one deliberately stops the app starting, and
thirty daily backups would carry every uploaded picture forever. Out in its own
directory a missing or broken picture falls back to the drawing and nobody loses
a booking over it. Do not move icons into the data file.

Three things hold the upload path together:

- **`IconSlots()` is the allowlist**, fixed in code. A file is written under the
  slot it was uploaded to, never under the name the browser sent, so there is no
  traversal to defend against — `handleIconFile` reconstructs the path from the
  same list. Adding a drawing to `iconSet` adds its slot automatically;
  `keycardred` and `keycardblue` are slots with no item of their own and fall
  back to the key drawing, which the board tints itself.
- **PNG is checked by magic bytes, not by extension**, and capped at
  `maxIconBytes`. Writes are temp-then-rename like the data file, so a
  half-received upload is never what the board picks up.
- **`iconMode` caches the mode and the file list** so `IconSVG` never reaches
  back into the Store. Templates render *after* the store lock is released, and
  a helper that took it again would deadlock the first time someone moved a
  render call inside a `Read`. Call `refreshIconsFromStore` after anything that
  changes either.

The keycard badge is two different pictures under a custom set, so
`/api/keycard` returns the rendered markup rather than letting the page work out
which to draw. Same reasoning as the category preview — one renderer, not two
that can drift apart.

`sample-icons/` holds plain black squares for testing the switch end to end.

## Print output

The cleaning sheet is eight pages: the keycard list once, the combined chart
once, then each of the three sections twice. The collection sheet at
`/inventory/print` is a separate single page, padded to the same 24 ruled rows.
Both copies of a section are deliberately identical — housekeepers split the
floors between themselves, so do not try to divide the rooms between the copies.

- Combined chart: six columns, room + marker for each of the three sections. No
  notes column.
- Section sheets: room, marker, notes. Notes is the wide column reception and
  housekeeping write in by hand.
- Title and date sit **inside** the table border as a spanning header row.
- Short sections are padded with blank ruled rows to a full page so every sheet
  looks the same.

Current widths: master 32/28mm per group, sections 30/28/122mm, rows 9.6mm,
body text 16px.

The browser's own URL and page-number headers are **not** controllable from
CSS. The print preview page tells the user to untick "Headers and footers" in
the print dialog. Don't attempt to fix this in code.

## Verifying print output

Reading the CSS is not verification — see invariant 3. Render it:

```sh
pip install weasyprint          # or use Chrome/Edge → Print → Save as PDF
./cleanlist &                   # note the port it prints
python3 - <<'EOF'
import weasyprint, urllib.request
P = 'http://127.0.0.1:8080'     # use the actual port
html = urllib.request.urlopen(P + '/print?date=2026-07-26').read().decode()
css  = urllib.request.urlopen(P + '/static/style.css').read().decode()
doc = weasyprint.HTML(string=html, base_url=P).render(
        stylesheets=[weasyprint.CSS(string=css)])
print('pages:', len(doc.pages))
doc.write_pdf('preview.pdf')
EOF
```

Then check the page count is still 8 and measure the columns rather than
eyeballing them:

```sh
pdftoppm -png -r 80 -f 2 -l 2 preview.pdf p
```

Weasyprint is a close but not exact match for Chromium's print engine. It is
good enough to catch column widths, row heights and page breaks; final say
belongs to the actual Windows box.

## Changing the data schema

`cleanlist-data.json` sits next to the executable and survives binary
replacement — dropping in a new `cleanlist.exe` keeps the data. By the time you
are reading this it may hold a full season of real occupancy, and there is only
one copy plus the daily backups.

**Before changing any struct that is serialised to that file** — `Stay`,
`Category`, `LongStay`, `ShortStay`, `State` — work out what happens to a file
written by the previous build. Adding a field with a sensible zero value is
safe. Renaming, removing, retyping or restructuring one is not.

Because a parse failure makes the app refuse to start (invariant 4, and rightly
so), a careless schema change turns into "Cleanlist won't open" at 8am on a
changeover day.

Rules for this:

- **Adding an optional field is fine.** Old files decode with the zero value.
  Make sure that zero value is the correct default, not merely a valid one.
- **Never repurpose an existing JSON tag.** If the meaning changes, use a new
  name and leave the old one readable.
- **A breaking change needs a `version` field and a migration.** Read the old
  shape, transform it, write the new shape, and take a backup copy first —
  something like `backups/pre-migration-<date>.json`, on top of the normal daily
  rotation.
- **Category ids are referenced by every stay.** Editing a category's `label` is
  safe. Changing its `id` orphans existing stays, which then silently resolve to
  `F` rather than erroring. If an id must change, rewrite the stays pointing at
  it in the same operation.
- **Test against a real file, not a fresh one.** Copy a populated
  `cleanlist-data.json` aside, run the new build against it, and confirm the
  board and a printed sheet both still resolve correctly. A passing unit suite
  proves nothing here — the tests build their state in memory and never touch
  the disk format.

If a migration is unavoidable, say so plainly rather than shipping it quietly.
The user needs to know to copy the data file aside before swapping the binary.

## Working style

- Run `go test ./...` before reporting a change complete.
- For anything touching print layout, render a PDF and state the measured
  numbers rather than the intended ones.
- Prefer boring solutions. This is a small tool with a small, non-technical
  audience and a long expected life.
- Greek text appears in the UI and on the sheets; keep it correct and don't
  transliterate it.

## Deliberately out of scope

ODT generation, guest names, booking references, PMS integration, network
access, authentication, service history, reporting, per-room exceptions like
DND. Housekeepers write exceptions on the paper. Don't add these without being
asked.

## Known open items

- Print layout is tuned empirically and may still need adjustment against the
  hotel's actual printer.
- The binary is unsigned, so Windows SmartScreen warns on first run. Only a code
  signing certificate removes this; itch.io hosting does not.
- Distribution is currently by USB stick. Email providers block `.exe`, even
  inside a zip. itch.io plus the itch app with `butler` push was discussed as a
  self-updating alternative for the reception PC.
