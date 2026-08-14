package main

import "testing"

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

func arrivals(list []roomStay) []string {
	out := []string{}
	for _, s := range list {
		out = append(out, s.Arrival)
	}
	return out
}
