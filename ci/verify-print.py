#!/usr/bin/env python3
"""Render the printed sheets and check them.

Invariant 3 in CLAUDE.md: print layout is verified by rendering, not by reading
CSS. `table-layout: fixed` takes its column widths from the first row, and the
first row of every print table is a spanning title cell — so without an explicit
<colgroup> the CSS widths are silently ignored and the columns collapse to equal
fractions. That bug shipped once and was invisible in review. It only showed up
on measurement.

This starts the binary in a scratch directory, seeds enough occupancy and enough
loans that every marker appears at least once, renders /print and
/inventory/print through weasyprint, and asserts the page counts. The PDFs are
written out for measurement by eye, which is still the last word on layout.

Usage:
    python3 ci/verify-print.py dist/cleanlist-linux --out out

Weasyprint is a close but not exact match for Chromium's print engine. It is
good enough to catch page counts, column widths and page breaks; final say
belongs to the hotel's actual printer.
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request

# A fixed date so the output is reproducible run to run.
DATE = "2026-07-26"

# Occupancy chosen so the rendered sheet exercises every marker. The comment on
# each line is what it should resolve to on DATE under the seeded categories.
STAYS = [
    (["100"], "booking", "2026-07-26", "2026-07-30"),  # AF   — arrives today
    (["101"], "booking", "2026-07-22", "2026-07-26"),  # AN   — departs today
    (["102"], "booking", "2026-07-22", "2026-07-26"),  # AN / AF, with the next
    (["102"], "booking", "2026-07-26", "2026-07-29"),  #        line: turnover
    (["201"], "booking", "2026-07-25", "2026-07-27"),  # F    — 2-night table is
    #                                                          empty, so all F
    (["202"], "booking", "2026-07-24", "2026-07-27"),  # P    — 3-night, day 3
    (["203"], "booking", "2026-07-24", "2026-07-28"),  # S & P — 4-night, day 3
    (["204"], "booking", "2026-07-20", "2026-07-30"),  # S & P — long stay, day 7
    (["205"], "booking", "2026-07-20", "2026-07-27"),  # P    — last-night
    #                                                          downgrade from S & P
    (["301"], "group", "2026-07-18", "2026-07-28"),    # S & P — other category,
    #                                                          3-day interval
]

# Loans covering due-today, overdue, per-room and named items.
LOANS = [
    ("iron-1", "201", "2026-07-26"),      # same day → due today
    ("safekey", "102", "2026-07-24"),     # checkout rule → due 26th from the stay
    ("hairdryer-1", "203", "2026-07-20"), # same day → six days overdue
]

# Markers that must appear somewhere in the rendered cleaning sheet. A missing
# one means either the seed above or the resolution logic changed.
EXPECT_MARKERS = ["AF", "AN", "AN / AF", "F", "P", "S &amp; P", "---"]

# DATE with the month-names setting on. Both sheets are rendered a second time
# in that mode because it is the wider date format and the collection sheet's
# due column is a fixed width.
EXPECT_MONTH = "Ιούλ"

# Keycards once, combined chart once, then each of the three sections twice.
EXPECT_PRINT_PAGES = 8

# Neither table on the collection sheet is padded, and the unpaid one rides
# under the collection list rather than claiming a page, so the seeded day fits
# on one sheet. It flows onto a second page only when the two lists together are
# genuinely long. Both states are checked: with rooms owing and with none, the
# count must stay 1 here, or something has started padding again.
EXPECT_COLLECT_PAGES = 1
EXPECT_COLLECT_PAID_PAGES = 1

# Every room with a guest in it on DATE, in printed order.
EXPECT_UNPAID_ROOMS = ["100", "101", "102", "201", "202", "203", "204", "205", "301"]

# Room 100 arrives on DATE and 102 turns over, so both need a card; 101 only
# departs and must not appear.
EXPECT_KEYCARD_ROOMS = ["100", "102"]


def start(binary):
    """Run the binary in a scratch dir and return (process, base_url).

    A scratch dir matters: the app writes cleanlist-data.json next to its own
    executable, so running it in place would touch a real installation's data.
    """
    tmp = tempfile.mkdtemp(prefix="cleanlist-verify-")
    exe = os.path.join(tmp, os.path.basename(binary))
    shutil.copy2(binary, exe)
    os.chmod(exe, 0o755)

    proc = subprocess.Popen(
        [exe], cwd=tmp, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        text=True, bufsize=1,
    )

    # The port is not fixed. Windows reserves blocks of ports at boot, so the
    # app walks a list and then asks the OS for anything free — the line it
    # prints is the only reliable source of the address.
    killer = threading.Timer(30, proc.kill)
    killer.start()
    url = None
    try:
        for line in proc.stdout:
            print("   [app]", line.rstrip())
            m = re.search(r"(http://127\.0\.0\.1:\d+)", line)
            if m:
                url = m.group(1)
                break
    finally:
        killer.cancel()

    if not url:
        proc.kill()
        raise SystemExit("the binary never printed an address — it failed to start")

    # Drain the rest of stdout in the background so the pipe never fills up.
    threading.Thread(target=lambda: [None for _ in proc.stdout], daemon=True).start()

    for _ in range(50):
        try:
            urllib.request.urlopen(url + "/", timeout=1).read()
            return proc, url, tmp
        except (urllib.error.URLError, OSError):
            time.sleep(0.1)
    proc.kill()
    raise SystemExit(f"{url} never became reachable")


def post(url, path, payload):
    req = urllib.request.Request(
        url + path,
        data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        return json.loads(urllib.request.urlopen(req, timeout=10).read())
    except urllib.error.HTTPError as e:
        body = e.read().decode(errors="replace")
        raise SystemExit(f"POST {path} failed: {e.code} {body}")


def get(url, path):
    return urllib.request.urlopen(url + path, timeout=10).read().decode()


def seed(url):
    for rooms, category, arrival, departure in STAYS:
        post(url, "/api/checkin", {
            "rooms": rooms, "category": category,
            "arrival": arrival, "departure": departure,
        })
    for item, room, lent_on in LOANS:
        post(url, "/api/lend", {
            "loans": [{"item": item, "room": room}],
            "lent_on": lent_on, "due_on": "", "note": "",
        })
    print(f"   seeded {len(STAYS)} stays and {len(LOANS)} loans")


def render(url, path, css, out_pdf):
    import weasyprint

    html = get(url, path)
    doc = weasyprint.HTML(string=html, base_url=url).render(
        stylesheets=[weasyprint.CSS(string=css)]
    )
    doc.write_pdf(out_pdf)
    return html, len(doc.pages)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("binary", help="path to the built Linux binary")
    ap.add_argument("--out", default="out", help="directory for the rendered PDFs")
    args = ap.parse_args()

    try:
        import weasyprint  # noqa: F401
    except ImportError:
        raise SystemExit("weasyprint is not installed — pip install weasyprint")

    os.makedirs(args.out, exist_ok=True)
    proc, url, tmp = start(args.binary)
    failures = []

    try:
        seed(url)
        css = get(url, "/static/style.css")

        print(f"\n-- cleaning sheet  /print?date={DATE}")
        html, pages = render(
            url, f"/print?date={DATE}", css,
            os.path.join(args.out, f"print-{DATE}.pdf"),
        )
        print(f"   pages: {pages} (expected {EXPECT_PRINT_PAGES})")
        if pages != EXPECT_PRINT_PAGES:
            failures.append(
                f"cleaning sheet rendered {pages} pages, expected "
                f"{EXPECT_PRINT_PAGES} (keycards, chart, three sections twice)"
            )
        missing = [m for m in EXPECT_MARKERS if m not in html]
        if missing:
            failures.append(f"markers missing from the sheet: {missing}")
        else:
            print(f"   markers present: {', '.join(EXPECT_MARKERS)}")

        # The keycard sheet is derived from arrivals, so a rules change that
        # stopped resolving AF would empty it silently.
        if "ΚΛΕΙΔΟΚΑΡΤΕΣ" not in html:
            failures.append("keycard sheet is missing from the print run")
        else:
            block = html.split("ΚΛΕΙΔΟΚΑΡΤΕΣ", 1)[1].split("</table>", 1)[0]
            listed = re.findall(r'<td class="c-room">([^<]+)</td>', block)
            if listed != EXPECT_KEYCARD_ROOMS:
                failures.append(
                    f"keycard sheet lists {listed}, expected {EXPECT_KEYCARD_ROOMS}"
                )
            else:
                print(f"   keycards: {', '.join(listed)}")

        print(f"\n-- collection sheet  /inventory/print?date={DATE}")
        html, pages = render(
            url, f"/inventory/print?date={DATE}", css,
            os.path.join(args.out, f"collection-{DATE}.pdf"),
        )
        print(f"   pages: {pages} (expected {EXPECT_COLLECT_PAGES})")
        if pages != EXPECT_COLLECT_PAGES:
            failures.append(
                f"collection sheet rendered {pages} pages, expected "
                f"{EXPECT_COLLECT_PAGES}"
            )
        for label in ("Κλειδί χρηματοκιβωτίου", "Σίδερο 1", "Σεσουάρ 1"):
            if label not in html:
                failures.append(f"collection sheet is missing {label!r}")

        # The unpaid page. Everything seeds unpaid, so it should list every
        # room occupied on DATE, in printed order.
        if "ΑΠΛΗΡΩΤΑ ΔΩΜΑΤΙΑ" not in html:
            failures.append("unpaid page is missing from the collection print")
        else:
            block = html.split("ΑΠΛΗΡΩΤΑ ΔΩΜΑΤΙΑ", 1)[1]
            listed = [r for r in re.findall(r'<td class="c-room">([^<]+)</td>', block)
                      if r.strip() and r != "&nbsp;"]
            if listed != EXPECT_UNPAID_ROOMS:
                failures.append(
                    f"unpaid page lists {listed}, expected {EXPECT_UNPAID_ROOMS}"
                )
            else:
                print(f"   unpaid: {', '.join(listed)}")

        # Both sheets again with month names switched on. This is the wider of
        # the two date formats -- an overdue line reads "26-Ιούλ-26 (+6)" -- and
        # the due column on the collection sheet is a fixed 3.6cm, so the mode
        # gets its own render rather than an argument about character counts.
        print("\n-- month names on")
        post(url, "/api/settings", {"month_names": True})
        for path, name, expect in (
            (f"/print?date={DATE}", "print", EXPECT_PRINT_PAGES),
            (f"/inventory/print?date={DATE}", "collection", EXPECT_COLLECT_PAGES),
        ):
            html, pages = render(
                url, path, css,
                os.path.join(args.out, f"{name}-months-{DATE}.pdf"),
            )
            print(f"   {name}: {pages} pages (expected {expect})")
            if pages != expect:
                failures.append(
                    f"{name} sheet with month names rendered {pages} pages, "
                    f"expected {expect} — the longer dates changed the layout"
                )
            if EXPECT_MONTH not in html:
                failures.append(
                    f"{name} sheet still shows a numeric month with the "
                    f"setting on — expected {EXPECT_MONTH!r}"
                )
        post(url, "/api/settings", {"month_names": False})

        # Everything paid: the unpaid page must disappear rather than print as
        # a page of empty ruled rows.
        print("\n-- all rooms paid")
        post(url, "/api/paid",
             {"rooms": EXPECT_UNPAID_ROOMS, "date": DATE, "paid": True})
        html, pages = render(
            url, f"/inventory/print?date={DATE}", css,
            os.path.join(args.out, f"collection-paid-{DATE}.pdf"),
        )
        print(f"   collection: {pages} pages (expected {EXPECT_COLLECT_PAID_PAGES})")
        if pages != EXPECT_COLLECT_PAID_PAGES:
            failures.append(
                f"with nothing owed the collection sheet rendered {pages} "
                f"pages, expected {EXPECT_COLLECT_PAID_PAGES}"
            )
        if "ΑΠΛΗΡΩΤΑ ΔΩΜΑΤΙΑ" in html:
            failures.append("unpaid page still prints with every room paid")
    finally:
        proc.kill()
        shutil.rmtree(tmp, ignore_errors=True)

    print()
    if failures:
        for f in failures:
            print(f"FAIL: {f}", file=sys.stderr)
        print(
            "\nThe PDFs are in "
            f"{args.out}/ — measure them rather than reading the CSS.",
            file=sys.stderr,
        )
        return 1

    print(f"OK — sheets rendered to {args.out}/")
    print("Page counts are checked automatically; column widths are not.")
    print("Measure those on the PDF when print layout changes.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
