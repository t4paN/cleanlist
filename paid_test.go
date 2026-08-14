package main

import (
	"reflect"
	"testing"
)

// A stay decoded from a file written before payment tracking existed is unpaid,
// because nobody has said otherwise. Unknown must never render as settled.
func TestStayDefaultsUnpaid(t *testing.T) {
	st := seedState()
	if err := AddStay(st, "210", Stay{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-15"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if st.Stays["210"][0].Paid {
		t.Fatal("a new stay must start unpaid")
	}
	if !UnpaidOn(st, "210", mustDate(t, "2026-08-12")) {
		t.Fatal("occupied room with an unpaid stay should read unpaid")
	}
}

func TestSetPaidAndBack(t *testing.T) {
	st := seedState()
	_ = AddStay(st, "210", Stay{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-15"})
	d := mustDate(t, "2026-08-12")

	if !SetPaid(st, "210", d, true) {
		t.Fatal("SetPaid found no stay to mark")
	}
	if UnpaidOn(st, "210", d) {
		t.Fatal("still unpaid after being marked paid")
	}
	// Reversible, or a mis-click loses track of a real debt.
	SetPaid(st, "210", d, false)
	if !UnpaidOn(st, "210", d) {
		t.Fatal("could not mark a room back to unpaid")
	}
}

// A vacant room has nothing to pay for, and SetPaid says so rather than
// reporting success on a no-op.
func TestSetPaidOnVacantRoom(t *testing.T) {
	st := seedState()
	if SetPaid(st, "210", mustDate(t, "2026-08-12"), true) {
		t.Fatal("SetPaid claimed to mark a room with no stay")
	}
	if UnpaidOn(st, "210", mustDate(t, "2026-08-12")) {
		t.Fatal("a vacant room must not read as unpaid")
	}
	if OccupiedOn(st, "210", mustDate(t, "2026-08-12")) {
		t.Fatal("a vacant room must not read as occupied")
	}
}

// On a turnover day two stays cover the date. Either one owing has to show, or
// a debt hides behind the other guest's settled bill.
func TestTurnoverNeedsBothPaid(t *testing.T) {
	st := seedState()
	_ = AddStay(st, "210", Stay{Category: "group", Arrival: "2026-08-05", Departure: "2026-08-10"})
	_ = AddStay(st, "210", Stay{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-15"})
	d := mustDate(t, "2026-08-10")

	// Mark only the outgoing guest.
	st.Stays["210"][0].Paid = true
	if !UnpaidOn(st, "210", d) {
		t.Fatal("room read as paid while the incoming guest still owes")
	}
	if s := UnpaidStay(st, "210", d); s == nil || s.Arrival != "2026-08-10" {
		t.Fatalf("UnpaidStay picked the wrong stay: %+v", s)
	}

	// The button marks every covering stay, so one press settles the day.
	SetPaid(st, "210", d, true)
	if UnpaidOn(st, "210", d) {
		t.Fatal("both covering stays should be paid after one press")
	}
}

// The printed list follows corridor order, like every other sheet.
func TestUnpaidRoomsInPrintedOrder(t *testing.T) {
	st := seedState()
	for _, room := range []string{"301", "102", "205", "100"} {
		if err := AddStay(st, room, Stay{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-15"}); err != nil {
			t.Fatalf("seed %s: %v", room, err)
		}
	}
	d := mustDate(t, "2026-08-12")

	want := []string{"100", "102", "205", "301"}
	if got := UnpaidRooms(st, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	SetPaid(st, "102", d, true)
	want = []string{"100", "205", "301"}
	if got := UnpaidRooms(st, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("after paying 102: got %v, want %v", got, want)
	}
}

// Nothing owed means an empty list, not a nil one — the collection template
// prints its extra page off the length, and a page of blank ruled rows on a day
// when everyone has paid is worse than no page at all.
func TestNoUnpaidRoomsIsEmpty(t *testing.T) {
	st := seedState()
	got := UnpaidRooms(st, mustDate(t, "2026-08-12"))
	if len(got) != 0 {
		t.Fatalf("expected nothing owed, got %v", got)
	}
}

// Paying for one date must not settle a different guest in the same room.
func TestPaidIsPerStayNotPerRoom(t *testing.T) {
	st := seedState()
	_ = AddStay(st, "210", Stay{Category: "group", Arrival: "2026-07-01", Departure: "2026-07-05"})
	_ = AddStay(st, "210", Stay{Category: "booking", Arrival: "2026-08-10", Departure: "2026-08-15"})

	SetPaid(st, "210", mustDate(t, "2026-07-03"), true)
	if !UnpaidOn(st, "210", mustDate(t, "2026-08-12")) {
		t.Fatal("paying July settled the August guest as well")
	}
}
