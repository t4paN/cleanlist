package main

import "testing"

func TestNeedsKeycard(t *testing.T) {
	for _, m := range []string{MarkerArrival, MarkerTurnover} {
		if !NeedsKeycard(m) {
			t.Fatalf("%q should need a keycard", m)
		}
	}
	for _, m := range []string{
		MarkerDeparture, MarkerFreshen, MarkerTowels, MarkerSheetsTow, MarkerVacant,
	} {
		if NeedsKeycard(m) {
			t.Fatalf("%q should not need a keycard", m)
		}
	}
}

// A card is needed wherever a guest arrives, and only there. A departure is a
// card coming back, and a mid-stay day is a guest who already has one.
func TestKeycardsForArrivalsAndTurnover(t *testing.T) {
	st := seedState()
	add := func(room, arrival, departure string) {
		t.Helper()
		if err := AddStay(st, room, Stay{
			Category: "booking", Arrival: arrival, Departure: departure,
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("100", "2026-08-12", "2026-08-15") // arriving  → card
	add("101", "2026-08-08", "2026-08-12") // departing → no card
	add("102", "2026-08-06", "2026-08-12") // turnover, both halves
	add("102", "2026-08-12", "2026-08-16") //           → card
	add("103", "2026-08-11", "2026-08-14") // mid-stay  → no card

	got := []string{}
	for _, k := range KeycardsFor(st, mustDate(t, "2026-08-12")) {
		got = append(got, k.Room+":"+k.Marker)
	}
	eq(t, got, []string{"100:" + MarkerArrival, "102:" + MarkerTurnover})
}

func TestKeycardsCarryTheCategoryLabel(t *testing.T) {
	st := seedState()
	if err := AddStay(st, "204", Stay{
		Category: "group", Arrival: "2026-08-12", Departure: "2026-08-15",
	}); err != nil {
		t.Fatal(err)
	}
	cards := KeycardsFor(st, mustDate(t, "2026-08-12"))
	if len(cards) != 1 {
		t.Fatalf("got %d cards, want 1", len(cards))
	}
	if cards[0].Category != "Group / Agency" {
		t.Fatalf("category = %q, want the label not the id", cards[0].Category)
	}
}

// The sheet is walked in the same corridor order as everything else.
func TestKeycardsFollowPrintedRoomOrder(t *testing.T) {
	st := seedState()
	for _, room := range []string{"301", "100", "205"} {
		if err := AddStay(st, room, Stay{
			Category: "booking", Arrival: "2026-08-12", Departure: "2026-08-14",
		}); err != nil {
			t.Fatal(err)
		}
	}
	got := []string{}
	for _, k := range KeycardsFor(st, mustDate(t, "2026-08-12")) {
		got = append(got, k.Room)
	}
	eq(t, got, []string{"100", "205", "301"})
}

func TestKeycardTicks(t *testing.T) {
	st := seedState()
	if IsBaked(st, "2026-08-12", "204") {
		t.Fatal("nothing should be ticked on a fresh state")
	}
	if !ToggleBaked(st, "2026-08-12", "204") {
		t.Fatal("the first toggle should tick it on")
	}
	if !IsBaked(st, "2026-08-12", "204") {
		t.Fatal("204 should be ticked")
	}
	// The same room on another date is a different card entirely.
	if IsBaked(st, "2026-08-13", "204") {
		t.Fatal("a tick must not carry across to another date")
	}
	if ToggleBaked(st, "2026-08-12", "204") {
		t.Fatal("the second toggle should clear it")
	}
	if IsBaked(st, "2026-08-12", "204") {
		t.Fatal("204 should be clear again")
	}
	if _, ok := st.Baked["2026-08-12"]; ok {
		t.Fatal("an emptied date should not be left behind in the data file")
	}
}

// Ticks are a same-day double-check, so they are bounded. Keeping a season of
// them would grow the data file for no benefit.
func TestOldTicksArePruned(t *testing.T) {
	st := seedState()
	st.Baked = map[string][]string{
		"2026-01-01": {"100"}, // well outside the window
		"2026-08-10": {"101"}, // two days ago
	}
	ToggleBaked(st, "2026-08-12", "102")

	if _, ok := st.Baked["2026-01-01"]; ok {
		t.Fatal("a tick from January should have been pruned")
	}
	if _, ok := st.Baked["2026-08-10"]; !ok {
		t.Fatal("a recent tick must be kept")
	}
	if !IsBaked(st, "2026-08-12", "102") {
		t.Fatal("the new tick should survive the prune it triggered")
	}
}

// Settings is a pointer so an old file is distinguishable from one where
// reception switched the check-off off. The default has to be on.
func TestDefaultSettingsEnableKeycardTracking(t *testing.T) {
	if !defaultSettings().KeycardTracking {
		t.Fatal("keycard tracking should default on")
	}
}
