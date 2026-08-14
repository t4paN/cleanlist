package main

import (
	"strconv"
	"time"
)

// ParseDate reads YYYY-MM-DD as a local calendar date at midnight.
func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, time.Local)
}

func FormatDate(t time.Time) string { return t.Format("2006-01-02") }

// FormatGreek renders DD-MM-YY, the way the hotel writes a date by hand. This
// is the only date format shown to a person — the board, the sheets, the
// collection list and the due dates all come through here, so changing it here
// changes it everywhere.
//
// It is display only. Dates are stored, sent to the API and put in <input
// type="date"> as ISO YYYY-MM-DD via FormatDate, and that must not change: the
// date picker refuses anything else and the day arithmetic parses it.
func FormatGreek(t time.Time) string { return t.Format("02-01-06") }

// DayNum converts a time to a whole-day ordinal built from its calendar
// components, deliberately re-anchored in UTC.
//
// This is the reason the app is correct twice a year. Computing day gaps by
// dividing a Duration by 24h breaks across the Greek DST transitions, where a
// day is 23 or 25 hours long, and would silently shift every service interval
// by one. Never reintroduce Duration arithmetic here.
func DayNum(t time.Time) int {
	y, m, d := t.Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// Cell is one resolved room row on the sheet.
type Cell struct {
	Room     string `json:"room"`
	Marker   string `json:"marker"`
	Category string `json:"category"` // label, empty when vacant
	Occupied bool   `json:"occupied"`
}

// Resolve determines the printed marker for a room on date d.
//
// Order: event markers (arrival / departure) win over service markers. A
// departing room is stripped regardless; an arriving room was made up before
// the guest walked in.
func Resolve(st *State, room string, d time.Time) Cell {
	dn := DayNum(d)
	var arriving, departing bool
	var covering *Stay

	for i := range st.Stays[room] {
		s := &st.Stays[room][i]
		a, err1 := ParseDate(s.Arrival)
		b, err2 := ParseDate(s.Departure)
		if err1 != nil || err2 != nil {
			continue
		}
		an, bn := DayNum(a), DayNum(b)
		if an == dn {
			arriving = true
			if covering == nil {
				covering = s
			}
		}
		if bn == dn {
			departing = true
			if covering == nil {
				covering = s
			}
		}
		if dn > an && dn < bn {
			covering = s
		}
	}

	label := ""
	if covering != nil {
		if c := st.Category(covering.Category); c != nil {
			label = c.Label
		}
	}

	switch {
	case departing && arriving:
		return Cell{room, MarkerTurnover, label, true}
	case departing:
		return Cell{room, MarkerDeparture, label, true}
	case arriving:
		return Cell{room, MarkerArrival, label, true}
	case covering == nil:
		return Cell{room, MarkerVacant, "", false}
	}

	cat := st.Category(covering.Category)
	if cat == nil {
		return Cell{room, MarkerFreshen, label, true}
	}
	a, _ := ParseDate(covering.Arrival)
	b, _ := ParseDate(covering.Departure)
	return Cell{room, ServiceMarker(cat, DayNum(b)-DayNum(a), dn-DayNum(a)+1), label, true}
}

// ServiceMarker resolves a mid-stay day.
//
// nights    total nights of the stay
// dayOfStay 1-based, where day 1 is the arrival day
//
// If nights is a key in the category's short-stay table, that table governs
// entirely: a listed day yields its marker, an unlisted day yields F, and the
// long-stay interval is not consulted. Otherwise the repeating interval applies.
//
// One rule sits above both: a full linen change falling on the guest's last
// night is downgraded to towels only. Fresh sheets on a bed that gets stripped
// the next morning are wasted, and the towels are what the guest still needs.
func ServiceMarker(cat *Category, nights, dayOfStay int) string {
	m := serviceMarkerRaw(cat, nights, dayOfStay)
	if dayOfStay == nights && m == MarkerSheetsTow {
		return MarkerTowels
	}
	return m
}

func serviceMarkerRaw(cat *Category, nights, dayOfStay int) string {
	if tbl, ok := cat.ShortStay[strconv.Itoa(nights)]; ok {
		if m, ok := tbl[strconv.Itoa(dayOfStay)]; ok && m != "" {
			return m
		}
		return MarkerFreshen
	}
	ls := cat.LongStay
	if ls.Interval > 0 && ls.FirstChangeDay > 0 &&
		dayOfStay >= ls.FirstChangeDay &&
		(dayOfStay-ls.FirstChangeDay)%ls.Interval == 0 {
		if ls.Marker != "" {
			return ls.Marker
		}
		return MarkerSheetsTow
	}
	return MarkerFreshen
}

// PreviewStay renders markers for every day of a hypothetical stay, used by the
// category editor so the rule is visible rather than imagined.
func PreviewStay(cat *Category, nights int) []string {
	out := make([]string, 0, nights+1)
	for day := 1; day <= nights+1; day++ {
		switch day {
		case 1:
			out = append(out, MarkerArrival)
		case nights + 1:
			out = append(out, MarkerDeparture)
		default:
			out = append(out, ServiceMarker(cat, nights, day))
		}
	}
	return out
}

// SectionCells resolves every room of a section for date d.
func SectionCells(st *State, sec Section, d time.Time) []Cell {
	out := make([]Cell, 0, len(sec.Rooms))
	for _, r := range sec.Rooms {
		out = append(out, Resolve(st, r, d))
	}
	return out
}
