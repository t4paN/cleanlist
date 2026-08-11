package main

import "testing"

func lendState(t *testing.T) *State {
	t.Helper()
	st := seedState()
	if err := AddStay(st, "204", Stay{Category: "booking", Arrival: "2026-08-01", Departure: "2026-08-06"}); err != nil {
		t.Fatal(err)
	}
	return st
}

// A checkout-rule item takes its due date from the room's stay. This is the
// whole reason inventory shares a binary with the cleaning board.
func TestDueDateFromStay(t *testing.T) {
	st := lendState(t)
	if err := LendItem(st, "safekey", "204", "2026-08-02", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := st.Loans[0].DueOn; got != "2026-08-06" {
		t.Fatalf("due = %q, want the stay's departure 2026-08-06", got)
	}
}

func TestSameDayRule(t *testing.T) {
	st := lendState(t)
	if err := LendItem(st, "iron-1", "204", "2026-08-02", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := st.Loans[0].DueOn; got != "2026-08-02" {
		t.Fatalf("due = %q, want 2026-08-02", got)
	}
}

// No stay on file means no invented date: an unexpected blank is easier to
// notice than a wrong due date.
func TestCheckoutRuleWithoutStay(t *testing.T) {
	st := seedState()
	if err := LendItem(st, "safekey", "310", "2026-08-02", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := st.Loans[0].DueOn; got != "" {
		t.Fatalf("due = %q, want empty", got)
	}
}

func TestExplicitDueOverridesRule(t *testing.T) {
	st := lendState(t)
	if err := LendItem(st, "iron-1", "204", "2026-08-02", "2026-08-04", ""); err != nil {
		t.Fatal(err)
	}
	if got := st.Loans[0].DueOn; got != "2026-08-04" {
		t.Fatalf("due = %q, want the explicit 2026-08-04", got)
	}
}

// A named item is one physical object, so it cannot be in two rooms at once.
func TestNamedItemCannotBeLentTwice(t *testing.T) {
	st := lendState(t)
	if err := LendItem(st, "iron-2", "204", "2026-08-02", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := LendItem(st, "iron-2", "201", "2026-08-02", "", ""); err == nil {
		t.Fatal("expected double-lend to be rejected")
	}
	if err := ReturnLoan(st, st.Loans[0].ID, "2026-08-02"); err != nil {
		t.Fatal(err)
	}
	if err := LendItem(st, "iron-2", "201", "2026-08-03", "", ""); err != nil {
		t.Fatalf("relend after return: %v", err)
	}
}

// A per-room item is one object per room, so the same item can be out to many
// rooms at once — but not twice to the same room.
func TestPerRoomItemIsPerRoom(t *testing.T) {
	st := lendState(t)
	if err := LendItem(st, "safekey", "204", "2026-08-02", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := LendItem(st, "safekey", "301", "2026-08-02", "", ""); err != nil {
		t.Fatalf("a different room's safe key must be lendable: %v", err)
	}
	if err := LendItem(st, "safekey", "204", "2026-08-02", "", ""); err == nil {
		t.Fatal("expected the same room's key to be rejected")
	}
}

func TestDoubleReturnRejected(t *testing.T) {
	st := lendState(t)
	_ = LendItem(st, "iron-1", "204", "2026-08-02", "", "")
	id := st.Loans[0].ID
	if err := ReturnLoan(st, id, "2026-08-02"); err != nil {
		t.Fatal(err)
	}
	if err := ReturnLoan(st, id, "2026-08-03"); err == nil {
		t.Fatal("expected second return to be rejected")
	}
}

// The collection sheet is due-today plus everything already overdue, in room
// order so one walk of the corridor collects the lot.
func TestCollections(t *testing.T) {
	st := lendState(t)
	_ = AddStay(st, "301", Stay{Category: "group", Arrival: "2026-08-01", Departure: "2026-08-09"})

	_ = LendItem(st, "iron-1", "301", "2026-08-01", "", "")      // due 08-01, overdue
	_ = LendItem(st, "safekey", "204", "2026-08-02", "", "")     // due 08-06, today
	_ = LendItem(st, "safekey", "301", "2026-08-02", "", "")     // due 08-09, not yet
	_ = LendItem(st, "hairdryer-2", "201", "2026-08-06", "", "") // due 08-06, today

	got := Collections(st, mustDate(t, "2026-08-06"))
	if len(got) != 3 {
		t.Fatalf("got %d items, want 3: %+v", len(got), got)
	}
	for i, w := range []string{"201", "204", "301"} {
		if got[i].Room != w {
			t.Fatalf("item %d room = %s, want %s", i, got[i].Room, w)
		}
	}
	if got[2].Status != StatusOverdue || got[2].DaysLate != 5 {
		t.Fatalf("iron should be 5 days overdue, got %s/%d", got[2].Status, got[2].DaysLate)
	}
	if got[0].ItemLabel != "Σεσουάρ 2" {
		t.Fatalf("label = %q, want the numbered name", got[0].ItemLabel)
	}
}

func TestReturnedNotCollected(t *testing.T) {
	st := lendState(t)
	_ = LendItem(st, "iron-1", "204", "2026-08-02", "", "")
	if n := len(Collections(st, mustDate(t, "2026-08-02"))); n != 1 {
		t.Fatalf("want 1 before return, got %d", n)
	}
	_ = ReturnLoan(st, st.Loans[0].ID, "2026-08-02")
	if n := len(Collections(st, mustDate(t, "2026-08-02"))); n != 0 {
		t.Fatalf("want 0 after return, got %d", n)
	}
}

func TestOpenEndedNeverDue(t *testing.T) {
	st := lendState(t)
	st.Items = append(st.Items, Item{"beach-1", "Ομπρέλα 1", ReturnOpen, 0, false, "umbrella"})
	_ = LendItem(st, "beach-1", "204", "2026-08-01", "", "")
	if n := len(Collections(st, mustDate(t, "2026-09-01"))); n != 0 {
		t.Fatalf("open-ended loan should never be collected, got %d", n)
	}
}

// The first inventory build stored numbered units. Those must become named
// items, with safe keys and remotes collapsing to per-room items, and any open
// loan must still point at something returnable.
func TestLegacyItemTypeMigration(t *testing.T) {
	st := &State{
		Stays:      map[string][]Stay{},
		Categories: seedState().Categories,
		Loans: []Loan{
			{ID: "l1", LegacyType: "iron", LegacyUnit: "2", Room: "204", LentOn: "2026-08-02", DueOn: "2026-08-02"},
			{ID: "l2", LegacyType: "safekey", LegacyUnit: "5", Room: "301", LentOn: "2026-08-02", DueOn: "2026-08-09"},
		},
		LegacyItemTypes: []legacyItemType{
			{"safekey", "Κλειδί χρηματοκιβωτίου", ReturnAtCheckout, 0, []string{"1", "2", "3"}},
			{"iron", "Σίδερο", ReturnSameDay, 0, []string{"1", "2"}},
		},
	}
	if !migrateItemTypes(st) {
		t.Fatal("expected migration to run")
	}
	if st.LegacyItemTypes != nil {
		t.Fatal("legacy types should be cleared")
	}

	// safekey collapses to one per-room item; irons expand to named items.
	if len(st.Items) != 3 {
		t.Fatalf("got %d items, want 3: %+v", len(st.Items), st.Items)
	}
	sk := st.Item("safekey")
	if sk == nil || !sk.PerRoom {
		t.Fatalf("safekey should survive as a per-room item: %+v", sk)
	}
	i2 := st.Item("iron-2")
	if i2 == nil || i2.PerRoom || i2.Label != "Σίδερο 2" {
		t.Fatalf("iron unit 2 should become named item %q: %+v", "Σίδερο 2", i2)
	}

	// Loans point at the new ids and carry no legacy residue.
	if st.Loans[0].Item != "iron-2" {
		t.Fatalf("loan 1 item = %q, want iron-2", st.Loans[0].Item)
	}
	if st.Loans[1].Item != "safekey" {
		t.Fatalf("loan 2 item = %q, want safekey", st.Loans[1].Item)
	}
	for _, l := range st.Loans {
		if l.LegacyType != "" || l.LegacyUnit != "" {
			t.Fatalf("legacy fields should be cleared: %+v", l)
		}
	}
	// The migrated loans must still be returnable.
	if err := ReturnLoan(st, "l1", "2026-08-03"); err != nil {
		t.Fatalf("migrated loan not returnable: %v", err)
	}
}

// A data file written before inventory existed gains items and keeps its stays.
func TestPreInventoryFileGainsItems(t *testing.T) {
	st := &State{Stays: map[string][]Stay{}, Categories: seedState().Categories}
	if migrateItemTypes(st) {
		t.Fatal("nothing to migrate on a pre-inventory file")
	}
	if st.Items == nil {
		st.Items = seedItems()
	}
	if len(st.Items) == 0 {
		t.Fatal("expected seeded items")
	}
}

// Icons are guessed from the item name so adding "Σίδερο 5" does not also
// require choosing a picture. Accents and case must not matter.
func TestGuessIcon(t *testing.T) {
	cases := map[string]string{
		"Σίδερο 2": "iron",
		"ΣΙΔΕΡΟ 7": "iron",
		"Κλειδί χρηματοκιβωτίου": "key",
		"Σεσουάρ 1":              "hairdryer",
		"Τηλεχειριστήριο":        "remote",
		"Extra towel":            "towel",
		"Phone charger":          "plug",
		"Κάτι άλλο":              "box",
	}
	for label, want := range cases {
		if got := guessIcon(label); got != want {
			t.Errorf("guessIcon(%q) = %q, want %q", label, got, want)
		}
	}
}

// An unknown icon id renders nothing rather than a broken placeholder.
func TestIconSVGUnknown(t *testing.T) {
	if got := IconSVG("no-such-icon"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
	if got := IconSVG("key"); got == "" {
		t.Fatal("known icon should render")
	}
}
