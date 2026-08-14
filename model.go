package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ---------- Markers ----------
// Single place to change the shorthand. AF/AN are Greek-derived (άφιξη /
// αναχώρηση), S/P map to σεντόνια/πετσέτες. All source literals are Latin
// characters so string comparison never surprises anyone.

const (
	MarkerArrival   = "AF"
	MarkerDeparture = "AN"
	MarkerFreshen   = "F"
	MarkerTowels    = "P"
	MarkerSheetsTow = "S & P"
	MarkerVacant    = "---"
	MarkerTurnover  = "AN / AF"
)

// AllMarkers are the choices offered in the category editor dropdowns.
//
// There is no sheets-only marker: the hotel never changes linen without also
// changing towels, but does change towels alone.
var AllMarkers = []string{MarkerFreshen, MarkerTowels, MarkerSheetsTow}

// legacyMarkers maps retired marker strings onto their replacements. Markers are
// stored as strings inside category rules, so renaming a constant is not enough
// — data written by an older build has to be rewritten on load.
var legacyMarkers = map[string]string{
	"S": MarkerTowels, // was sheets-only, retired in favour of towels-only
}

// migrateMarkers rewrites retired marker strings in place. Returns true if
// anything changed, so the caller knows to persist.
func migrateMarkers(st *State) bool {
	changed := false
	for i := range st.Categories {
		c := &st.Categories[i]
		if repl, ok := legacyMarkers[c.LongStay.Marker]; ok {
			c.LongStay.Marker = repl
			changed = true
		}
		for nights, days := range c.ShortStay {
			for day, m := range days {
				if repl, ok := legacyMarkers[m]; ok {
					c.ShortStay[nights][day] = repl
					changed = true
				}
			}
		}
	}
	return changed
}

// ---------- Rooms (static) ----------

type Section struct {
	Key   string
	Label string
	Rooms []string
}

func rng(prefix string, from, to int) []string {
	out := []string{}
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("%s%02d", prefix, i))
	}
	return out
}

var Sections = []Section{
	{"ground", "Ισόγειο / Παράρτημα", []string{"100", "101", "102", "103", "104", "105", "106", "400", "401", "A01", "A02"}},
	{"floor2", "2ος Όροφος", rng("2", 1, 24)},
	{"floor3", "3ος Όροφος", rng("3", 1, 24)},
}

func AllRooms() []string {
	out := []string{}
	for _, s := range Sections {
		out = append(out, s.Rooms...)
	}
	return out
}

func SectionOf(room string) string {
	for _, s := range Sections {
		for _, r := range s.Rooms {
			if r == room {
				return s.Key
			}
		}
	}
	return ""
}

// ---------- Stays ----------

type Stay struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Arrival   string `json:"arrival"`   // YYYY-MM-DD
	Departure string `json:"departure"` // YYYY-MM-DD

	// Paid records that reception has taken the money for this stay. It is
	// additive and unpaid is the zero value, which is the only honest default:
	// a stay written by an older build is one nobody has confirmed payment
	// for, and an unknown must never render as settled. The board shows unpaid
	// occupied rooms in red, so on the first run after an upgrade the rooms
	// with guests in them light up until reception works through them once.
	Paid bool `json:"paid"`
}

// ---------- Categories ----------

// ShortStay is keyed by total nights, then by day of stay (1 = arrival day).
// A nights key that exists but has no entry for a given day yields "F".
// A nights key that does not exist at all falls through to LongStay.
type ShortStay map[string]map[string]string

type LongStay struct {
	FirstChangeDay int    `json:"first_change_day"`
	Interval       int    `json:"interval"`
	Marker         string `json:"marker"`
}

type Category struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	LongStay  LongStay  `json:"long_stay"`
	ShortStay ShortStay `json:"short_stay"`
}

// ---------- State ----------

type State struct {
	Stays      map[string][]Stay `json:"stays"`
	Categories []Category        `json:"categories"`

	// Inventory. Added after the cleaning board shipped; both fields decode as
	// empty from an older data file and are seeded on load, so existing
	// installations keep their stays and gain the item list.
	Items []Item `json:"items"`
	Loans []Loan `json:"loans"`

	// LegacyItemTypes holds the shape used by the first inventory build, where
	// items had numbered units. Read only so that data can be migrated; never
	// written back.
	LegacyItemTypes []legacyItemType `json:"item_types,omitempty"`

	// Baked records which keycards reception has ticked off, keyed by date and
	// listing rooms. Purely additive: a file written before keycards existed
	// decodes as nil, and nil correctly means nothing has been ticked.
	Baked map[string][]string `json:"baked,omitempty"`

	// Settings is a pointer so that a file written before settings existed is
	// distinguishable from one where reception has deliberately switched
	// everything off. Absent gets the defaults; present is left alone.
	Settings *Settings `json:"settings,omitempty"`
}

// Settings holds the preferences reception changes from the burger menu.
type Settings struct {
	// KeycardTracking turns on the double-click check-off on the board. The
	// printed sheet crossed off by hand is the record; this is a second pass
	// for reception to confirm against, so it defaults on and can be switched
	// off by anyone who finds it noise.
	KeycardTracking bool `json:"keycard_tracking"`

	// CustomIcons switches the board and the sheets over to the pictures in the
	// icons/ directory. Off is both the intended default and the zero value, so
	// a file written by the previous build correctly keeps the drawings.
	CustomIcons bool `json:"custom_icons"`

	// MonthNames writes the month as a Greek abbreviation — 14-Αύγ-26 instead
	// of 14-08-26 — everywhere a date is shown, the printed sheets included.
	// Off is both the intended default and the zero value, so an existing file
	// keeps the all-numeric dates it already had.
	MonthNames bool `json:"month_names"`
}

func defaultSettings() *Settings { return &Settings{KeycardTracking: true} }

// ---------- Keycard ticks ----------

func IsBaked(st *State, date, room string) bool {
	for _, r := range st.Baked[date] {
		if r == room {
			return true
		}
	}
	return false
}

// ToggleBaked flips one room's tick for one date and reports the new state.
func ToggleBaked(st *State, date, room string) bool {
	if st.Baked == nil {
		st.Baked = map[string][]string{}
	}
	list := st.Baked[date]
	for i, r := range list {
		if r == room {
			rest := append(list[:i:i], list[i+1:]...)
			if len(rest) == 0 {
				delete(st.Baked, date)
			} else {
				st.Baked[date] = rest
			}
			return false
		}
	}
	st.Baked[date] = append(list, room)
	pruneBaked(st, date)
	return true
}

// keepBakedDays bounds how long ticks are kept. They are a same-day
// double-check, so holding a whole season of them only grows the data file.
const keepBakedDays = 90

func pruneBaked(st *State, today string) {
	t, err := ParseDate(today)
	if err != nil {
		return
	}
	cutoff := DayNum(t) - keepBakedDays
	for date := range st.Baked {
		d, err := ParseDate(date)
		if err != nil || DayNum(d) < cutoff {
			delete(st.Baked, date)
		}
	}
}

func seedState() *State {
	short := func() ShortStay {
		return ShortStay{
			"2": {},
			"3": {"3": MarkerTowels},
			"4": {"3": MarkerSheetsTow},
		}
	}
	return &State{
		Stays: map[string][]Stay{},
		Categories: []Category{
			{"booking", "Booking.com", LongStay{3, 2, MarkerSheetsTow}, short()},
			{"group", "Group / Agency", LongStay{3, 3, MarkerSheetsTow}, short()},
			{"other", "Other", LongStay{3, 3, MarkerSheetsTow}, short()},
		},
		Items: seedItems(),
		Loans: []Loan{},
	}
}

func (s *State) Category(id string) *Category {
	for i := range s.Categories {
		if s.Categories[i].ID == id {
			return &s.Categories[i]
		}
	}
	return nil
}

func (s *State) CategoryInUse(id string) []string {
	rooms := []string{}
	for room, stays := range s.Stays {
		for _, st := range stays {
			if st.Category == id {
				rooms = append(rooms, room)
				break
			}
		}
	}
	sort.Strings(rooms)
	return rooms
}

func (s *State) clone() *State {
	b, _ := json.Marshal(s)
	var out State
	_ = json.Unmarshal(b, &out)
	if out.Stays == nil {
		out.Stays = map[string][]Stay{}
	}
	if out.Loans == nil {
		out.Loans = []Loan{}
	}
	return &out
}

// ---------- Store ----------

type Store struct {
	mu       sync.Mutex
	state    *State
	undo     *State
	path     string
	backups  string
	lastBkup string
}

func dataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func NewStore() (*Store, error) {
	dir := dataDir()
	st := &Store{
		path:    filepath.Join(dir, "cleanlist-data.json"),
		backups: filepath.Join(dir, "backups"),
	}
	if err := st.load(); err != nil {
		return nil, err
	}
	return st, nil
}

// ErrLoad is returned when the data file exists but cannot be read. The caller
// must surface this loudly: silently starting with an empty board would look
// like every room is vacant and could go unnoticed until housekeeping is out.
type ErrLoad struct {
	Path    string
	Backups string
	Err     error
}

func (e *ErrLoad) Error() string {
	return fmt.Sprintf("could not read %s: %v", e.Path, e.Err)
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.state = seedState()
		return s.persist()
	}
	if err != nil {
		return &ErrLoad{s.path, s.backups, err}
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return &ErrLoad{s.path, s.backups, err}
	}
	if st.Stays == nil {
		st.Stays = map[string][]Stay{}
	}
	if len(st.Categories) == 0 {
		st.Categories = seedState().Categories
	}
	// A data file written before inventory existed has neither field. Seed the
	// items so the tab is usable, and leave loans empty.
	seeded := migrateItemTypes(&st)
	if st.Items == nil {
		st.Items = seedItems()
		seeded = true
	}
	if st.Loans == nil {
		st.Loans = []Loan{}
		seeded = true
	}
	// A file written before the burger menu existed has no settings block.
	// Seeding it rather than letting the zero value stand is the difference
	// between the keycard check-off defaulting on, as intended, and defaulting
	// off because bool happens to start false.
	if st.Settings == nil {
		st.Settings = defaultSettings()
		seeded = true
	}
	s.state = &st
	// Write the upgraded shape straight back, so the file on disk always
	// matches what the app is holding rather than only after the first edit.
	if migrateMarkers(s.state) || seeded {
		return s.persist()
	}
	return nil
}

// persist writes atomically: temp file, fsync, rename. Never truncate in place.
func (s *Store) persist() error {
	s.rotateBackup()
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// rotateBackup copies the existing file aside once per calendar day, then
// prunes to the most recent 30.
func (s *Store) rotateBackup() {
	today := time.Now().Format("2006-01-02")
	if s.lastBkup == today {
		return
	}
	// Note the flag only after a backup actually lands. On the very first run
	// there is no file to copy, and setting it early would skip day one.
	cur, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.backups, 0o755); err != nil {
		return
	}
	s.lastBkup = today
	_ = os.WriteFile(filepath.Join(s.backups, "cleanlist-"+today+".json"), cur, 0o644)

	entries, err := os.ReadDir(s.backups)
	if err != nil {
		return
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > 30 {
		_ = os.Remove(filepath.Join(s.backups, names[0]))
		names = names[1:]
	}
}

// Mutate snapshots state for undo, applies fn, and persists on success.
func (s *Store) Mutate(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.state.clone()
	if err := fn(s.state); err != nil {
		s.state = snapshot // roll back partial changes
		return err
	}
	s.undo = snapshot
	return s.persist()
}

// MutateNoUndo applies a change and persists it without touching the undo
// snapshot.
//
// Undo holds exactly one step, and it exists to protect stay data — a Check Out
// on a mis-selected range is unreconstructable. Ticking a keycard or flipping a
// setting is bookkeeping, and letting either consume the single undo slot would
// quietly disarm the safety net between selecting the wrong rooms and noticing.
func (s *Store) MutateNoUndo(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.state.clone()
	if err := fn(s.state); err != nil {
		s.state = snapshot // roll back partial changes
		return err
	}
	return s.persist()
}

func (s *Store) Undo() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.undo == nil {
		return fmt.Errorf("nothing to undo")
	}
	s.state, s.undo = s.undo, nil
	return s.persist()
}

func (s *Store) CanUndo() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.undo != nil
}

func (s *Store) Read(fn func(*State)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s.state)
}

// ---------- Stay helpers ----------

func nextID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// AddStay validates against overlap. Sharing a single date, where one stay's
// departure equals the next stay's arrival, is the one permitted adjacency.
func AddStay(st *State, room string, s Stay) error {
	a, err := ParseDate(s.Arrival)
	if err != nil {
		return fmt.Errorf("room %s: bad arrival date", room)
	}
	d, err := ParseDate(s.Departure)
	if err != nil {
		return fmt.Errorf("room %s: bad departure date", room)
	}
	if !d.After(a) {
		return fmt.Errorf("room %s: departure must be after arrival", room)
	}
	if st.Category(s.Category) == nil {
		return fmt.Errorf("unknown category %q", s.Category)
	}
	na, nd := DayNum(a), DayNum(d)
	for _, ex := range st.Stays[room] {
		ea, _ := ParseDate(ex.Arrival)
		ed, _ := ParseDate(ex.Departure)
		xa, xd := DayNum(ea), DayNum(ed)
		// Overlap unless they merely touch at a single shared date.
		if na < xd && nd > xa {
			return fmt.Errorf("room %s: overlaps existing stay %s → %s",
				room, ex.Arrival, ex.Departure)
		}
	}
	s.ID = nextID()
	st.Stays[room] = append(st.Stays[room], s)
	sort.Slice(st.Stays[room], func(i, j int) bool {
		return st.Stays[room][i].Arrival < st.Stays[room][j].Arrival
	})
	return nil
}

// CoversDate reports whether s spans d, counting the arrival and departure days
// as covered. Day ordinals, not Duration — see DayNum.
func CoversDate(s *Stay, d time.Time) bool {
	a, err1 := ParseDate(s.Arrival)
	b, err2 := ParseDate(s.Departure)
	if err1 != nil || err2 != nil {
		return false
	}
	dn := DayNum(d)
	return dn >= DayNum(a) && dn <= DayNum(b)
}

// OccupiedOn reports whether any stay covers d. This is what makes the Paid
// button available: a vacant room has nothing to pay for.
func OccupiedOn(st *State, room string, d time.Time) bool {
	for i := range st.Stays[room] {
		if CoversDate(&st.Stays[room][i], d) {
			return true
		}
	}
	return false
}

// UnpaidOn reports whether any stay covering d is still unpaid.
//
// Any, not the first: on a turnover day the outgoing and the incoming guest
// both cover the date, and either one owing money has to show. Hiding a debt
// behind the other guest's settled bill is the failure that matters here.
func UnpaidOn(st *State, room string, d time.Time) bool {
	for i := range st.Stays[room] {
		s := &st.Stays[room][i]
		if CoversDate(s, d) && !s.Paid {
			return true
		}
	}
	return false
}

// UnpaidStay returns the unpaid stay covering d, or nil. On a turnover day
// where one guest has paid and the other has not, this is the one that owes.
func UnpaidStay(st *State, room string, d time.Time) *Stay {
	for i := range st.Stays[room] {
		s := &st.Stays[room][i]
		if CoversDate(s, d) && !s.Paid {
			return s
		}
	}
	return nil
}

// UnpaidRooms lists every occupied room whose stay is unpaid on d, in printed
// room order so the sheet reads down the corridor like the others.
func UnpaidRooms(st *State, d time.Time) []string {
	out := []string{}
	for _, room := range AllRooms() {
		if UnpaidOn(st, room, d) {
			out = append(out, room)
		}
	}
	return out
}

// SetPaid marks every stay covering d in the given room, and reports whether it
// found one. A room with no stay on the date is refused by the caller rather
// than passed over quietly — a click that silently does nothing reads as a
// click that worked.
func SetPaid(st *State, room string, d time.Time, paid bool) bool {
	found := false
	for i := range st.Stays[room] {
		s := &st.Stays[room][i]
		if CoversDate(s, d) {
			s.Paid = paid
			found = true
		}
	}
	return found
}

// CurrentStay returns the stay covering d, counting arrival and departure days
// as covered. Used by the Check Out action.
func CurrentStay(st *State, room string, d time.Time) *Stay {
	for i := range st.Stays[room] {
		s := &st.Stays[room][i]
		if CoversDate(s, d) {
			return s
		}
	}
	return nil
}

// ---------- Legacy inventory migration ----------

// legacyItemType is the first inventory shape: one entry per kind of thing,
// with individually numbered units. It was replaced because the hotel writes
// the number on the object itself ("Σίδερο 2"), and because safe keys belong to
// a room rather than to a pool.
type legacyItemType struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	ReturnRule string   `json:"return_rule"`
	ReturnDays int      `json:"return_days"`
	Units      []string `json:"units"`
}

// perRoomLegacy lists legacy types that become per-room items rather than a
// numbered set. Their units are discarded; the room supplies the identity.
var perRoomLegacy = map[string]bool{"safekey": true, "remote": true}

// migrateItemTypes converts the numbered-unit shape into named items, rewriting
// any open loans to point at the new ids. Returns true if anything changed.
func migrateItemTypes(st *State) bool {
	if len(st.LegacyItemTypes) == 0 {
		return false
	}
	items := []Item{}
	// unitToItem maps "type|unit" onto the id that replaces it.
	unitToItem := map[string]string{}

	for _, lt := range st.LegacyItemTypes {
		if perRoomLegacy[lt.ID] {
			items = append(items, Item{lt.ID, lt.Label, lt.ReturnRule, lt.ReturnDays, true, guessIcon(lt.Label)})
			for _, u := range lt.Units {
				unitToItem[lt.ID+"|"+u] = lt.ID
			}
			continue
		}
		for _, u := range lt.Units {
			id := lt.ID + "-" + u
			label := lt.Label + " " + u
			items = append(items, Item{id, label, lt.ReturnRule, lt.ReturnDays, false, guessIcon(label)})
			unitToItem[lt.ID+"|"+u] = id
		}
	}

	// Loans written by the old build carry a "unit" field that no longer has a
	// home on Loan, so it is recovered from the raw JSON by the caller-side
	// decode into legacyLoanUnits. Where that is unavailable the loan is left
	// pointing at its old type id, which the item lookup then reports plainly.
	for i := range st.Loans {
		l := &st.Loans[i]
		if l.Item != "" {
			continue
		}
		if id, ok := unitToItem[l.LegacyType+"|"+l.LegacyUnit]; ok {
			l.Item = id
		} else {
			l.Item = l.LegacyType
		}
		l.LegacyType, l.LegacyUnit = "", ""
	}

	st.Items = items
	st.LegacyItemTypes = nil
	return true
}
