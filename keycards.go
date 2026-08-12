package main

import "time"

// A keycard has to be encoded for every guest who arrives, which means every
// room resolving to AF or AN / AF on the day.
//
// None of this is stored. The list is derived from the same stays the cleaning
// board reads, so it cannot drift out of step with the sheet printed beside it,
// and a stay edited after the sheet was printed produces a corrected list on the
// next print rather than a stale one.
//
// The single piece of state is the tick — see Baked in model.go. That exists
// only so reception can confirm it worked through the whole list; the printed
// sheet, crossed off by hand, is still the record.

type Keycard struct {
	Room     string
	Category string // label, empty when the category is unknown
	Marker   string // AF or AN / AF
	Baked    bool
}

// NeedsKeycard reports whether a resolved marker means a guest is arriving.
// Turnover counts: the room changes hands, so the incoming guest needs a card
// regardless of the outgoing one handing theirs back.
func NeedsKeycard(marker string) bool {
	return marker == MarkerArrival || marker == MarkerTurnover
}

// KeycardsFor returns the rooms needing a card on d, in printed room order so
// the list follows the same corridor walk as every other sheet.
func KeycardsFor(st *State, d time.Time) []Keycard {
	date := FormatDate(d)
	out := []Keycard{}
	for _, room := range AllRooms() {
		c := Resolve(st, room, d)
		if !NeedsKeycard(c.Marker) {
			continue
		}
		out = append(out, Keycard{
			Room:     room,
			Category: c.Category,
			Marker:   c.Marker,
			Baked:    IsBaked(st, date, room),
		})
	}
	return out
}
