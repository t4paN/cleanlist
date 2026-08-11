package main

import "testing"

// Stays may be entered in any order. Adding a later stay first and then
// back-filling an earlier one that departs on its arrival date must behave
// identically to entering them chronologically.
func TestInsertionOrderIrrelevant(t *testing.T) {
	rev := seedState()
	if err := AddStay(rev, "210", Stay{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-15"}); err != nil {
		t.Fatalf("later stay: %v", err)
	}
	if err := AddStay(rev, "210", Stay{Category: "group", Arrival: "2026-08-05", Departure: "2026-08-10"}); err != nil {
		t.Fatalf("back-filled earlier stay rejected: %v", err)
	}

	fwd := seedState()
	_ = AddStay(fwd, "210", Stay{Category: "group", Arrival: "2026-08-05", Departure: "2026-08-10"})
	_ = AddStay(fwd, "210", Stay{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-15"})

	for _, d := range []string{"2026-08-05", "2026-08-07", "2026-08-08", "2026-08-10", "2026-08-12", "2026-08-15", "2026-08-16"} {
		day := mustDate(t, d)
		a, b := Resolve(rev, "210", day), Resolve(fwd, "210", day)
		if a.Marker != b.Marker {
			t.Fatalf("%s: reverse-entered %q, chronological %q", d, a.Marker, b.Marker)
		}
		t.Logf("%s -> %s", d, a.Marker)
	}
	// The stored order is normalised regardless of entry order.
	if rev.Stays["210"][0].Arrival != "2026-08-05" {
		t.Fatalf("stays not sorted by arrival: %v", rev.Stays["210"])
	}
}
