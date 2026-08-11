# Cleanlist

Daily housekeeping sheet generator. Enter stays once, print the per-section
sheets already filled in.

## Running it

Copy `cleanlist.exe` anywhere on the reception PC — a folder of its own is best,
because it writes its data alongside itself — and double-click it.

A console window opens and stays open. **That window is the app running; closing
it stops Cleanlist.** The browser opens automatically.

The address is usually `http://127.0.0.1:8080`, but **read the port from the
console window** — it isn't always 8080. Windows reserves blocks of ports for
Hyper-V, WSL and Docker at boot, and a reserved port fails to bind even though
nothing is listening on it. Cleanlist walks a list of alternatives and then asks
the OS for any free port, so it starts regardless. The line it prints is the
address to use.

If it fails for some other reason, the window now stays open with the error
rather than vanishing.

### First launch on Windows

Two one-time prompts, both expected:

- **"Windows protected your PC"** — SmartScreen, because the binary is unsigned.
  *More info → Run anyway.*
- **Windows Firewall** — may or may not appear. Cleanlist binds to `127.0.0.1`
  only and is not reachable from the network, so declining is fine.

### Files it creates

```
cleanlist.exe
cleanlist-data.json     ← all occupancy and category data
backups/                ← one snapshot per day, last 30 kept
```

Back up `cleanlist-data.json` the same way you'd back up anything else that
matters. It is plain text and can be opened in Notepad.

## Daily use

1. **Check In** — select rooms (click, or shift-click for a range within one
   column), press Check In, choose the category and the arrival/departure dates.
2. **Print** — pick the date if it isn't today, press Print. Six pages come out:
   each of the three sections twice.
3. **Check Out** is only for guests leaving *earlier* than booked. Normal
   departures already have a date from check-in and need no action, so you can
   print at 8am for a room that leaves at 11am.

Above the board is a **daily total** for the selected date: F, P and S & P show
how much linen the day needs, then AN and AN / AF show how many rooms need a
full strip and turnaround. It follows the date picker, so changing the date
shows tomorrow's numbers.

Arrivals (AF) are not counted — the room was made up before the guest arrived,
so it is not work for that day.

**Undo** reverses the last action. It holds one step only.

## Markers

| | |
|---|---|
| `AF` | άφιξη — arrival |
| `AN` | αναχώρηση — departure |
| `F` | freshen up |
| `P` | towels only |
| `S & P` | sheets and towels |
| `AN / AF` | departure and arrival the same day |
| `---` | empty room |

## Inventory

The **Inventory** link opens the loanable-items board.

Items come in two shapes:

- **Named items** — irons, hairdryers. Each physical object is its own entry
  with its number in the name ("Σίδερο 2"), because that is what is written on
  the object. One item is one thing, so it cannot be in two rooms at once.
- **Per-room items** — safe keys, remotes. These belong to a room and never
  travel, so the room is the identity. They appear as a grid matching the
  cleaning board: click room 204 to lend 204 its own key.

Actions:

- **Lend** — select named items or rooms, press Lend. Named items ask which
  room; per-room items already know. The due date fills itself in from the
  item's rule, so leave the due field blank unless this loan is an exception.
- **Return** — select things that are out and press Return, or use the button
  on the collection list.
- **Print collections** — one page listing everything due back on the selected
  date plus anything already overdue, in room order so a single walk of the
  corridor collects the lot. Overdue lines show how many days late.

Colours: white in store, grey out, amber due today, red overdue.

Each item carries a small icon, shown on the board and on the printed sheet so
a line can be read at a glance. Adding an item picks one from its name — call
something "Σίδερο 5" and it gets the iron — and the picker in the editor
overrides that.

**Items** opens the editor. Each item has a return rule:

| Rule | Due back |
|---|---|
| At checkout | when the guest departs, read from the cleaning board |
| Same day | the day it went out |
| Fixed days | N days after lending |
| Open ended | never appears on a collection sheet |

"At checkout" is why inventory and the cleaning board share one program: lending
a safe key to a room already booked out on Friday needs no extra typing. If the
room has no stay on file the due date is left blank rather than guessed — an
unexpected blank is easier to spot than a wrong date.

An item still out on loan cannot be deleted.

## Categories

`Categories` in the top-right. Each one carries its own rules, so an agency with
its own contract gets its own entry.

- **Short stays** are listed by total nights and govern that length completely.
  Arrival and departure days are shown fixed; you set the markers on the days
  between.
- **Repeating interval** applies to any stay length *not* listed as a short stay.
  The preview strip below it shows how a long stay resolves.

A full change (`S & P`) that falls on a guest's **last night** is automatically
reduced to `P`. Fresh sheets on a bed that gets stripped the next morning are
wasted, and this happens regardless of category or how the rules are set.

Press **Save**. A category still attached to a stay can't be deleted — reassign
those rooms first.

Shipped with Booking.com (change every 2 days), Group / Agency and Other (every
3). All three are editable.

## Rooms

Hardcoded in `model.go`, in `Sections`:

- Ισόγειο / Παράρτημα — 100–106, 400, 401, A01, A02
- 2ος Όροφος — 201–224
- 3ος Όροφος — 301–324

Changing them means a rebuild.

## Building

```sh
go test ./...
GOOS=windows GOARCH=amd64 go build -o cleanlist.exe .
```

No dependencies beyond the standard library. `go build` alone gives a Linux
binary for testing on your own machine — behaviour is identical apart from the
browser-launch call.

### Things worth knowing before you change it

- **Never compute day gaps by dividing a `Duration` by 24h.** Use `DayNum` in
  `rules.go`. Greek DST makes some days 23 or 25 hours long and the naive version
  shifts every service interval by one, twice a year. `TestDSTBoundaries` guards
  this.
- `time/tzdata` is imported blank in `main.go` because Windows ships no timezone
  database.
- The category editor gets its preview from `/api/preview` rather than
  reimplementing the rules in JavaScript. Two copies would eventually disagree,
  and the printed sheet is the one that matters.
- Writes are atomic (temp file → fsync → rename) and a corrupt data file makes
  the app refuse to start rather than show an empty board.

## Not included

ODT output, guest names, PMS integration, network access, service history,
per-room exceptions like DND. Housekeepers write those on the paper.
