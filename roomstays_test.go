package main

import (
	"fmt"
	"strings"
	"testing"
)

// The detail panel is read to answer "who is in room 210 right now", so the
// current stay comes first and the rest run newest-first below it.
func TestRoomStaysCurrentFirst(t *testing.T) {
	st := seedState()
	for _, s := range []Stay{
		{Category: "group", Arrival: "2026-07-01", Departure: "2026-07-05"},
		{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-15"},
		{Category: "other", Arrival: "2026-09-20", Departure: "2026-09-25"},
	} {
		if err := AddStay(st, "210", s); err != nil {
			t.Fatalf("seed %s: %v", s.Arrival, err)
		}
	}

	got := RoomStays(st, "210", mustDate(t, "2026-08-12"))
	want := []string{"2026-08-10", "2026-09-20", "2026-07-01"}
	if len(got) != len(want) {
		t.Fatalf("got %d stays, want %d", len(got), len(want))
	}
	for i, arr := range want {
		if got[i].Arrival != arr {
			t.Fatalf("position %d: got %s, want %s (order %v)", i, got[i].Arrival, arr, arrivals(got))
		}
	}
	// Only the covering stay is bold on the page.
	if !got[0].Current {
		t.Fatalf("stay covering the date not marked current")
	}
	for _, s := range got[1:] {
		if s.Current {
			t.Fatalf("%s marked current but does not cover 2026-08-12", s.Arrival)
		}
	}

	// The stored order is what the overlap check and Resolve read; it must stay
	// ascending by arrival.
	if st.Stays["210"][0].Arrival != "2026-07-01" {
		t.Fatalf("RoomStays reordered stored stays: %v", st.Stays["210"])
	}
}

// Departure day and arrival day both count as covered, so on a turnover both
// stays are current. The incoming one goes on top.
func TestRoomStaysTurnoverDay(t *testing.T) {
	st := seedState()
	_ = AddStay(st, "210", Stay{Category: "group", Arrival: "2026-08-05", Departure: "2026-08-10"})
	_ = AddStay(st, "210", Stay{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-15"})

	got := RoomStays(st, "210", mustDate(t, "2026-08-10"))
	if got[0].Arrival != "2026-08-10" || !got[0].Current {
		t.Fatalf("incoming stay not first and current: %v", arrivals(got))
	}
	if !got[1].Current {
		t.Fatalf("outgoing stay should still count as current on its departure day")
	}
}

// A room with nothing booked returns an empty list, not nil — the panel prints
// "No stays recorded." off the length.
func TestRoomStaysEmpty(t *testing.T) {
	if got := RoomStays(seedState(), "210", mustDate(t, "2026-08-12")); len(got) != 0 {
		t.Fatalf("expected no stays, got %v", arrivals(got))
	}
}

// Dates are shown to people as DD-MM-YY and stored as ISO. Both halves matter:
// the date pickers and the API only accept ISO, so a display format leaking
// into FormatDate would break check-in.
func TestDisplayDateFormat(t *testing.T) {
	d := mustDate(t, "2026-08-14")
	if got := FormatGreek(d); got != "14-08-26" {
		t.Fatalf("display format: got %q, want %q", got, "14-08-26")
	}
	if got := FormatDate(d); got != "2026-08-14" {
		t.Fatalf("stored format changed: got %q, want %q", got, "2026-08-14")
	}
	// Single-digit days and months keep their leading zero, so the column
	// stays the same width on the printed sheet.
	if got := FormatGreek(mustDate(t, "2026-01-05")); got != "05-01-26" {
		t.Fatalf("leading zeros: got %q, want %q", got, "05-01-26")
	}
}

// With the menu option on, the month is written out. Off is the default and the
// zero value, so an existing data file keeps the numeric dates it had.
func TestMonthNamesSetting(t *testing.T) {
	defer RefreshDateFormat(false)

	RefreshDateFormat(true)
	if !MonthNamesOn() {
		t.Fatal("setting did not take")
	}
	if got := FormatGreek(mustDate(t, "2026-08-14")); got != "14-Αύγ-26" {
		t.Fatalf("got %q, want %q", got, "14-Αύγ-26")
	}
	if got := FormatGreek(mustDate(t, "2026-01-05")); got != "05-Ιαν-26" {
		t.Fatalf("leading zero on the day: got %q, want %q", got, "05-Ιαν-26")
	}

	RefreshDateFormat(false)
	if got := FormatGreek(mustDate(t, "2026-08-14")); got != "14-08-26" {
		t.Fatalf("switching back: got %q, want %q", got, "14-08-26")
	}
	if defaultSettings().MonthNames {
		t.Fatal("month names must default off")
	}
}

// Every month has an abbreviation and June and July are distinguishable — the
// obvious copy-paste error in a hand-written table.
func TestGreekMonthTable(t *testing.T) {
	defer RefreshDateFormat(false)
	RefreshDateFormat(true)

	seen := map[string]bool{}
	for i := 1; i <= 12; i++ {
		d := mustDate(t, fmt.Sprintf("2026-%02d-15", i))
		got := FormatGreek(d)
		name := strings.Split(got, "-")[1]
		if name == "" {
			t.Fatalf("month %d has no abbreviation", i)
		}
		if seen[name] {
			t.Fatalf("month %d repeats the abbreviation %q", i, name)
		}
		seen[name] = true
		t.Logf("%2d -> %s", i, got)
	}
}

// The panel carries both: formatted dates to show, ISO to send back.
func TestRoomStaysCarryDisplayDates(t *testing.T) {
	st := seedState()
	if err := AddStay(st, "210", Stay{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-18"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := RoomStays(st, "210", mustDate(t, "2026-08-14"))[0]
	if got.ArrivalGr != "10-08-26" || got.DepartureGr != "18-08-26" {
		t.Fatalf("display dates: got %q → %q", got.ArrivalGr, got.DepartureGr)
	}
	if got.Arrival != "2026-08-10" || got.Departure != "2026-08-18" {
		t.Fatalf("ISO dates must survive for the delete call: %q → %q", got.Arrival, got.Departure)
	}
}

// A date that will not parse shows as stored rather than as a blank. An
// unreadable date is a data problem worth seeing.
func TestDisplayDateFallsBackToStored(t *testing.T) {
	if got := displayDate("not-a-date"); got != "not-a-date" {
		t.Fatalf("got %q, want the stored text back", got)
	}
}

func arrivals(list []roomStay) []string {
	out := []string{}
	for _, s := range list {
		out = append(out, s.Arrival)
	}
	return out
}
