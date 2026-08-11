package main

import (
	"fmt"
	"sort"
	"time"
)

// ---------- Return rules ----------
//
// Each item carries a default rule for when it is due back. The rule is a
// default, not a constraint: any individual loan can override it.

const (
	ReturnAtCheckout = "checkout" // due when the guest departs
	ReturnSameDay    = "same_day" // due back the day it went out
	ReturnAfterDays  = "days"     // due back N days after lending
	ReturnOpen       = "open"     // no due date
)

var ReturnRules = []string{ReturnAtCheckout, ReturnSameDay, ReturnAfterDays, ReturnOpen}

func ReturnRuleLabel(r string) string {
	switch r {
	case ReturnAtCheckout:
		return "At checkout"
	case ReturnSameDay:
		return "Same day"
	case ReturnAfterDays:
		return "Fixed days"
	case ReturnOpen:
		return "Open ended"
	}
	return r
}

// ---------- Items ----------

// Item is one loanable thing. There is no unit numbering: a hotel with four
// irons has four items called "Σίδερο 1".."Σίδερο 4", because that is what is
// written on the iron.
//
// PerRoom items are the exception. A safe key belongs to its room and is never
// lent anywhere else, so one item stands for the whole set and the room
// supplies the identity. Lending it to 204 means 204's own key is out.
type Item struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	ReturnRule string `json:"return_rule"`
	ReturnDays int    `json:"return_days"` // only used when rule is "days"
	PerRoom    bool   `json:"per_room"`
	Icon       string `json:"icon"`
}

// Loan is one item out to one room. Returned is empty while it is out.
type Loan struct {
	ID       string `json:"id"`
	Item     string `json:"item"`
	Room     string `json:"room"`
	LentOn   string `json:"lent_on"`  // YYYY-MM-DD
	DueOn    string `json:"due_on"`   // YYYY-MM-DD, empty when open ended
	Returned string `json:"returned"` // YYYY-MM-DD, empty while out
	Note     string `json:"note"`

	// Fields written by the first inventory build, kept only so its loans can
	// be migrated. Cleared once Item is resolved, and omitempty keeps them out
	// of anything written afterwards.
	LegacyType string `json:"type,omitempty"`
	LegacyUnit string `json:"unit,omitempty"`
}

func (l *Loan) Out() bool { return l.Returned == "" }

func seedItems() []Item {
	items := []Item{
		{"safekey", "Κλειδί χρηματοκιβωτίου", ReturnAtCheckout, 0, true, "key"},
		{"remote", "Τηλεχειριστήριο", ReturnAtCheckout, 0, true, "remote"},
	}
	for i := 1; i <= 4; i++ {
		items = append(items, Item{
			fmt.Sprintf("iron-%d", i), fmt.Sprintf("Σίδερο %d", i), ReturnSameDay, 0, false, "iron",
		})
	}
	for i := 1; i <= 6; i++ {
		items = append(items, Item{
			fmt.Sprintf("hairdryer-%d", i), fmt.Sprintf("Σεσουάρ %d", i), ReturnSameDay, 0, false, "hairdryer",
		})
	}
	return items
}

func (s *State) Item(id string) *Item {
	for i := range s.Items {
		if s.Items[i].ID == id {
			return &s.Items[i]
		}
	}
	return nil
}

// ---------- Due dates ----------

// DueDate resolves when a loan should come back. Returns an empty string for
// open-ended loans.
//
// The checkout rule is why inventory lives in the same binary as the cleaning
// board: it reads the room's stay directly, so lending a safe key to a room
// already booked out on Friday needs no extra typing.
func DueDate(st *State, it *Item, room, lentOn string) string {
	lent, err := ParseDate(lentOn)
	if err != nil {
		return ""
	}
	switch it.ReturnRule {
	case ReturnSameDay:
		return lentOn
	case ReturnAfterDays:
		n := it.ReturnDays
		if n < 0 {
			n = 0
		}
		return FormatDate(lent.AddDate(0, 0, n))
	case ReturnAtCheckout:
		if s := CurrentStay(st, room, lent); s != nil {
			return s.Departure
		}
		// No stay on file. Leave it open rather than inventing a date —
		// an unexpected blank is easier to notice than a wrong date.
		return ""
	}
	return ""
}

// ---------- Loan queries ----------

const (
	StatusDue     = "due"
	StatusOverdue = "overdue"
	StatusOut     = "out"
)

func loanStatus(l *Loan, on time.Time) string {
	if l.DueOn == "" {
		return StatusOut
	}
	due, err := ParseDate(l.DueOn)
	if err != nil {
		return StatusOut
	}
	switch d := DayNum(on) - DayNum(due); {
	case d > 0:
		return StatusOverdue
	case d == 0:
		return StatusDue
	}
	return StatusOut
}

// CollectionItem is one line of the collection sheet.
type CollectionItem struct {
	Loan
	ItemLabel string
	ItemIcon  string
	Status    string
	DaysLate  int
	DueGr     string // due date in DD/MM/YYYY, matching the printed sheets
}

// Collections returns everything that should come back on or before the given
// date — due today plus anything already overdue — sorted into printed room
// order so a single walk of the corridor collects the lot.
func Collections(st *State, on time.Time) []CollectionItem {
	out := []CollectionItem{}
	for i := range st.Loans {
		l := st.Loans[i]
		if !l.Out() {
			continue
		}
		status := loanStatus(&l, on)
		if status != StatusDue && status != StatusOverdue {
			continue
		}
		label, icon := l.Item, ""
		if it := st.Item(l.Item); it != nil {
			label, icon = it.Label, it.Icon
		}
		late := 0
		if status == StatusOverdue {
			if due, err := ParseDate(l.DueOn); err == nil {
				late = DayNum(on) - DayNum(due)
			}
		}
		dueGr := ""
		if due, err := ParseDate(l.DueOn); err == nil {
			dueGr = FormatGreek(due)
		}
		out = append(out, CollectionItem{l, label, icon, status, late, dueGr})
	}
	sortCollections(out)
	return out
}

func sortCollections(items []CollectionItem) {
	order := map[string]int{}
	for i, r := range AllRooms() {
		order[r] = i
	}
	sort.SliceStable(items, func(a, b int) bool {
		ra, okA := order[items[a].Room]
		rb, okB := order[items[b].Room]
		if !okA {
			ra = 1 << 30
		}
		if !okB {
			rb = 1 << 30
		}
		if ra != rb {
			return ra < rb
		}
		return items[a].ItemLabel < items[b].ItemLabel
	})
}

// ---------- Boards ----------

// RoomCell is one room's state for a per-room item.
type RoomCell struct {
	Room   string `json:"room"`
	Status string `json:"status"` // "" when in store
	DueGr  string `json:"due_gr"`
	LoanID string `json:"loan_id"`
}

// RoomBoard is a per-room item shown as a grid of rooms, matching the layout of
// the cleaning board so the two read the same way.
type RoomBoard struct {
	Item
	RuleLabel string
	Sections  []RoomBoardSection
	OutCount  int
}

type RoomBoardSection struct {
	Label string
	Cells []RoomCell
}

// ItemState is one named item on the shared items strip.
type ItemState struct {
	Item
	RuleLabel string
	Room      string
	Status    string
	DueGr     string
	LoanID    string
}

func openLoans(st *State) map[string][]*Loan {
	open := map[string][]*Loan{}
	for i := range st.Loans {
		l := &st.Loans[i]
		if l.Out() {
			open[l.Item] = append(open[l.Item], l)
		}
	}
	return open
}

func dueGr(l *Loan) string {
	if due, err := ParseDate(l.DueOn); err == nil {
		return FormatGreek(due)
	}
	return ""
}

// RoomBoards builds one room grid per per-room item.
func RoomBoards(st *State, on time.Time) []RoomBoard {
	open := openLoans(st)
	boards := []RoomBoard{}
	for _, it := range st.Items {
		if !it.PerRoom {
			continue
		}
		byRoom := map[string]*Loan{}
		for _, l := range open[it.ID] {
			byRoom[l.Room] = l
		}
		b := RoomBoard{Item: it, RuleLabel: ReturnRuleLabel(it.ReturnRule)}
		for _, sec := range Sections {
			bs := RoomBoardSection{Label: sec.Label}
			for _, r := range sec.Rooms {
				c := RoomCell{Room: r}
				if l, ok := byRoom[r]; ok {
					c.Status = loanStatus(l, on)
					c.DueGr = dueGr(l)
					c.LoanID = l.ID
					b.OutCount++
				}
				bs.Cells = append(bs.Cells, c)
			}
			b.Sections = append(b.Sections, bs)
		}
		boards = append(boards, b)
	}
	return boards
}

// NamedItems builds the strip of individually named items.
func NamedItems(st *State, on time.Time) []ItemState {
	open := openLoans(st)
	out := []ItemState{}
	for _, it := range st.Items {
		if it.PerRoom {
			continue
		}
		s := ItemState{Item: it, RuleLabel: ReturnRuleLabel(it.ReturnRule)}
		if ls := open[it.ID]; len(ls) > 0 {
			l := ls[0]
			s.Room = l.Room
			s.Status = loanStatus(l, on)
			s.DueGr = dueGr(l)
			s.LoanID = l.ID
		}
		out = append(out, s)
	}
	return out
}

// ---------- Mutations ----------

// LendItem records a loan.
//
// A named item is a single physical object, so it cannot be in two rooms at
// once. A per-room item is one object per room, so the same check applies per
// room rather than globally.
func LendItem(st *State, itemID, room, lentOn, dueOn, note string) error {
	it := st.Item(itemID)
	if it == nil {
		return fmt.Errorf("unknown item %q", itemID)
	}
	if SectionOf(room) == "" {
		return fmt.Errorf("unknown room %q", room)
	}
	if _, err := ParseDate(lentOn); err != nil {
		return fmt.Errorf("bad lending date")
	}
	if dueOn != "" {
		if _, err := ParseDate(dueOn); err != nil {
			return fmt.Errorf("bad due date")
		}
	}
	for i := range st.Loans {
		l := &st.Loans[i]
		if !l.Out() || l.Item != itemID {
			continue
		}
		if it.PerRoom {
			if l.Room == room {
				return fmt.Errorf("%s for room %s is already out", it.Label, room)
			}
			continue
		}
		return fmt.Errorf("%s is already out to room %s", it.Label, l.Room)
	}
	if dueOn == "" {
		dueOn = DueDate(st, it, room, lentOn)
	}
	st.Loans = append(st.Loans, Loan{
		ID:     nextID(),
		Item:   itemID,
		Room:   room,
		LentOn: lentOn,
		DueOn:  dueOn,
		Note:   note,
	})
	return nil
}

// ReturnLoan closes a loan. Returning something already returned is an error
// rather than a no-op, because it usually means the wrong row was clicked.
func ReturnLoan(st *State, id, on string) error {
	if _, err := ParseDate(on); err != nil {
		return fmt.Errorf("bad date")
	}
	for i := range st.Loans {
		if st.Loans[i].ID == id {
			if !st.Loans[i].Out() {
				return fmt.Errorf("already returned on %s", st.Loans[i].Returned)
			}
			st.Loans[i].Returned = on
			return nil
		}
	}
	return fmt.Errorf("loan not found")
}

// ItemInUse lists rooms holding an item, used to block deletion.
func (s *State) ItemInUse(id string) []string {
	rooms := []string{}
	seen := map[string]bool{}
	for i := range s.Loans {
		l := &s.Loans[i]
		if l.Item == id && l.Out() && !seen[l.Room] {
			seen[l.Room] = true
			rooms = append(rooms, l.Room)
		}
	}
	sort.Strings(rooms)
	return rooms
}
