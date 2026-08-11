package main

import (
	"testing"
	"time"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatalf("bad date %s: %v", s, err)
	}
	return d
}

// stayRun walks a stay day by day and returns the markers in order.
func stayRun(t *testing.T, st *State, room, arrival, departure, cat string) []string {
	t.Helper()
	if err := AddStay(st, room, Stay{Category: cat, Arrival: arrival, Departure: departure}); err != nil {
		t.Fatalf("AddStay: %v", err)
	}
	a := mustDate(t, arrival)
	b := mustDate(t, departure)
	out := []string{}
	for d := a; !d.After(b); d = d.AddDate(0, 0, 1) {
		out = append(out, Resolve(st, room, d).Marker)
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length %d, want %d\ngot  %v\nwant %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("day %d = %q, want %q\ngot  %v\nwant %v", i+1, got[i], want[i], got, want)
		}
	}
}

func TestTwoNightBooking(t *testing.T) {
	st := seedState()
	eq(t, stayRun(t, st, "201", "2026-05-04", "2026-05-06", "booking"),
		[]string{"AF", "F", "AN"})
}

// Three nights gets towels only. There is no sheets-only marker: linen is never
// changed without towels, but towels are changed alone.
func TestThreeNightBooking(t *testing.T) {
	st := seedState()
	eq(t, stayRun(t, st, "202", "2026-05-04", "2026-05-07", "booking"),
		[]string{"AF", "F", "P", "AN"})
}

func TestFourNightBooking(t *testing.T) {
	st := seedState()
	eq(t, stayRun(t, st, "203", "2026-05-04", "2026-05-08", "booking"),
		[]string{"AF", "F", "S & P", "F", "AN"})
}

// Long stays fall through to the repeating interval: first change on day 3,
// then every 2 days.
func TestTenNightBooking(t *testing.T) {
	st := seedState()
	eq(t, stayRun(t, st, "204", "2026-05-04", "2026-05-14", "booking"),
		[]string{"AF", "F", "S & P", "F", "S & P", "F", "S & P", "F", "S & P", "F", "AN"})
}

// Group carries interval 3, so the same dates resolve differently.
func TestCategoriesResolveIndependently(t *testing.T) {
	st := seedState()
	book := stayRun(t, st, "205", "2026-05-04", "2026-05-14", "booking")
	grp := stayRun(t, st, "206", "2026-05-04", "2026-05-14", "group")
	eq(t, grp, []string{"AF", "F", "S & P", "F", "F", "S & P", "F", "F", "S & P", "F", "AN"})
	if book[4] == grp[4] {
		t.Fatalf("expected day 5 to differ between categories, both %q", book[4])
	}
}

func TestSameDayTurnover(t *testing.T) {
	st := seedState()
	if err := AddStay(st, "207", Stay{Category: "booking", Arrival: "2026-05-04", Departure: "2026-05-07"}); err != nil {
		t.Fatal(err)
	}
	if err := AddStay(st, "207", Stay{Category: "group", Arrival: "2026-05-07", Departure: "2026-05-10"}); err != nil {
		t.Fatalf("touching stays must be allowed: %v", err)
	}
	if got := Resolve(st, "207", mustDate(t, "2026-05-07")).Marker; got != "AN / AF" {
		t.Fatalf("turnover = %q, want %q", got, "AN / AF")
	}
}

func TestOverlapRejected(t *testing.T) {
	st := seedState()
	if err := AddStay(st, "208", Stay{Category: "booking", Arrival: "2026-05-04", Departure: "2026-05-08"}); err != nil {
		t.Fatal(err)
	}
	if err := AddStay(st, "208", Stay{Category: "booking", Arrival: "2026-05-06", Departure: "2026-05-09"}); err == nil {
		t.Fatal("expected overlap to be rejected")
	}
}

func TestVacant(t *testing.T) {
	st := seedState()
	if got := Resolve(st, "301", mustDate(t, "2026-05-04")).Marker; got != "---" {
		t.Fatalf("vacant = %q, want ---", got)
	}
}

// The DST guard. Greece springs forward on the last Sunday of March and falls
// back on the last Sunday of October. A stay spanning either transition must
// produce exactly the same day numbering as one that spans neither. If day
// arithmetic ever regresses to dividing a Duration by 24h, these fail.
func TestDSTBoundaries(t *testing.T) {
	athens, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		t.Skip("tzdata unavailable:", err)
	}
	old := time.Local
	time.Local = athens
	defer func() { time.Local = old }()

	control := seedState()
	want := stayRun(t, control, "220", "2026-05-04", "2026-05-14", "booking")

	// Spring forward: 29 March 2026.
	spring := seedState()
	eq(t, stayRun(t, spring, "221", "2026-03-25", "2026-04-04", "booking"), want)

	// Fall back: 25 October 2026.
	autumn := seedState()
	eq(t, stayRun(t, autumn, "222", "2026-10-21", "2026-10-31", "booking"), want)
}

func TestPreviewStay(t *testing.T) {
	st := seedState()
	cat := st.Category("booking")
	eq(t, PreviewStay(cat, 3), []string{"AF", "F", "P", "AN"})
	eq(t, PreviewStay(cat, 2), []string{"AF", "F", "AN"})
}

func TestUnlistedShortDayIsFreshen(t *testing.T) {
	st := seedState()
	cat := st.Category("booking")
	// 3 nights lists only day 3; day 2 must fall back to F and must not
	// consult the long-stay interval.
	if got := ServiceMarker(cat, 3, 2); got != MarkerFreshen {
		t.Fatalf("day 2 of 3-night = %q, want F", got)
	}
}

// A data file written before the sheets-only marker was retired must be
// rewritten on load rather than left printing a marker that no longer exists.
func TestLegacyMarkerMigration(t *testing.T) {
	st := seedState()
	st.Categories[0].ShortStay["3"]["3"] = "S"
	st.Categories[0].LongStay.Marker = "S"
	st.Categories[1].ShortStay["4"]["3"] = MarkerSheetsTow

	if !migrateMarkers(st) {
		t.Fatal("expected migration to report a change")
	}
	if got := st.Categories[0].ShortStay["3"]["3"]; got != MarkerTowels {
		t.Fatalf("short stay marker = %q, want %q", got, MarkerTowels)
	}
	if got := st.Categories[0].LongStay.Marker; got != MarkerTowels {
		t.Fatalf("long stay marker = %q, want %q", got, MarkerTowels)
	}
	if got := st.Categories[1].ShortStay["4"]["3"]; got != MarkerSheetsTow {
		t.Fatalf("untouched marker changed to %q", got)
	}
	if migrateMarkers(st) {
		t.Fatal("migration should be a no-op the second time")
	}
}

// A full linen change landing on the last night is downgraded to towels only:
// fresh sheets on a bed stripped the next morning are wasted.
func TestLastNightDowngrade(t *testing.T) {
	st := seedState()

	// Booking.com: interval 2 from day 3, so a 5-night stay would change on
	// days 3 and 5. Day 5 is the last night.
	eq(t, stayRun(t, st, "230", "2026-05-04", "2026-05-09", "booking"),
		[]string{"AF", "F", "S & P", "F", "P", "AN"})

	// Group: interval 3 from day 3, so a 6-night stay changes on days 3 and 6.
	eq(t, stayRun(t, st, "231", "2026-05-04", "2026-05-10", "group"),
		[]string{"AF", "F", "S & P", "F", "F", "P", "AN"})

	// A change day that is not the last night is untouched.
	eq(t, stayRun(t, st, "232", "2026-05-04", "2026-05-14", "booking"),
		[]string{"AF", "F", "S & P", "F", "S & P", "F", "S & P", "F", "S & P", "F", "AN"})
}

// The downgrade applies to explicit short-stay entries too, not only to the
// repeating interval.
func TestLastNightDowngradeAppliesToShortStay(t *testing.T) {
	st := seedState()
	cat := st.Category("booking")
	cat.ShortStay["4"]["4"] = MarkerSheetsTow
	if got := ServiceMarker(cat, 4, 4); got != MarkerTowels {
		t.Fatalf("last night of 4-night stay = %q, want %q", got, MarkerTowels)
	}
	if got := ServiceMarker(cat, 4, 3); got != MarkerSheetsTow {
		t.Fatalf("day 3 of 4-night stay = %q, want %q", got, MarkerSheetsTow)
	}
}
