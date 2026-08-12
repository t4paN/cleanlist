package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	// Windows ships no tzdata, and LoadLocation would fail there. Embedding it
	// costs ~450KB and keeps the DST arithmetic correct on the reception PC.
	_ "time/tzdata"
)

//go:embed static/*
var assets embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"greek":       FormatGreek,
	"markerClass": markerClass,
	"icon":        IconSVG,
}).ParseFS(assets, "static/*.html"))

// markerClass maps a marker to its CSS class. Kept out of the template so the
// marker strings stay defined in exactly one place.
func markerClass(m string) string {
	switch m {
	case MarkerVacant:
		return "mk-vacant"
	case MarkerSheetsTow:
		return "mk-SP"
	case MarkerTurnover:
		return "mk-turn"
	case MarkerTowels:
		return "mk-P"
	case MarkerArrival:
		return "mk-AF"
	case MarkerDeparture:
		return "mk-AN"
	default:
		return "mk-F"
	}
}

// Preferred ports, tried in order. Windows reserves blocks of ports for
// Hyper-V/WSL/Docker at boot, and binding one fails with WSAEACCES even though
// nothing is listening on it. Rather than make the user diagnose that, walk a
// list and then let the OS pick anything free.
var preferredPorts = []int{8080, 8081, 8090, 8321, 9080, 17280}

var store *Store

func listen() (net.Listener, error) {
	var lastErr error
	for _, p := range preferredPorts {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			return ln, nil
		}
		lastErr = err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		return ln, nil
	}
	return nil, fmt.Errorf("no usable port found (last error: %v)", lastErr)
}

// fatal keeps the console window open. A window that flashes and vanishes tells
// whoever is standing at reception nothing at all.
func fatal(format string, args ...any) {
	fmt.Println()
	fmt.Printf(format+"\n", args...)
	fmt.Println()
	fmt.Println("Press Enter to close this window.")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	os.Exit(1)
}

func main() {
	s, err := NewStore()
	if err != nil {
		var le *ErrLoad
		if errors.As(err, &le) {
			serveLoadError(le)
			return
		}
		fatal("Cleanlist could not start: %v", err)
	}
	store = s
	refreshIconsFromStore()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleBoard)
	mux.HandleFunc("/categories", handleCategoriesPage)
	mux.HandleFunc("/inventory", handleInventory)
	mux.HandleFunc("/inventory/print", handleCollectionPrint)
	mux.HandleFunc("/api/lend", jsonPost(apiLend))
	mux.HandleFunc("/api/return", jsonPost(apiReturn))
	mux.HandleFunc("/api/items", jsonPost(apiSaveItems))
	mux.HandleFunc("/print", handlePrint)
	mux.HandleFunc("/api/checkin", jsonPost(apiCheckIn))
	mux.HandleFunc("/api/checkout", jsonPost(apiCheckOut))
	mux.HandleFunc("/api/stay/delete", jsonPost(apiDeleteStay))
	mux.HandleFunc("/api/undo", jsonPost(apiUndo))
	mux.HandleFunc("/api/keycard", jsonPost(apiKeycardToggle))
	mux.HandleFunc("/api/settings", jsonPost(apiSettings))
	// "/icons" is the editor and "/icons/" serves the files. Registering both
	// is how ServeMux distinguishes an exact path from a subtree.
	mux.HandleFunc("/icons", handleIconsPage)
	mux.HandleFunc("/icons/", handleIconFile)
	mux.HandleFunc("/api/icons/upload", handleIconUpload)
	mux.HandleFunc("/api/icons/remove", handleIconRemove)
	mux.HandleFunc("/api/categories", jsonPost(apiSaveCategories))
	mux.HandleFunc("/api/preview", jsonPost(apiPreview))
	mux.HandleFunc("/api/room", apiRoom)
	mux.HandleFunc("/static/", http.FileServer(http.FS(assets)).ServeHTTP)

	ln, err := listen()
	if err != nil {
		fatal("Cleanlist could not start: %v", err)
	}
	url := "http://" + ln.Addr().String()
	fmt.Println("Cleanlist running at", url)
	fmt.Println("Close this window to stop.")
	openBrowser(url)
	if err := http.Serve(ln, mux); err != nil {
		fatal("Cleanlist stopped: %v", err)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// serveLoadError stands up a minimal server that shows nothing but the failure.
// Starting with an empty board would look like every room is vacant.
func serveLoadError(le *ErrLoad) {
	h := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = tmpl.ExecuteTemplate(w, "error.html", map[string]any{
			"Message": le.Error(),
			"Backups": le.Backups,
		})
	}
	fmt.Println("DATA FILE PROBLEM:", le.Error())
	fmt.Println("Backups are in:", le.Backups)
	ln, err := listen()
	if err != nil {
		fatal("Cleanlist could not start: %v", err)
	}
	openBrowser("http://" + ln.Addr().String())
	if err := http.Serve(ln, http.HandlerFunc(h)); err != nil {
		fatal("Cleanlist stopped: %v", err)
	}
}

// ---------- Pages ----------

func queryDate(r *http.Request) time.Time {
	if s := r.URL.Query().Get("date"); s != "" {
		if d, err := ParseDate(s); err == nil {
			return d
		}
	}
	n := time.Now()
	y, m, dd := n.Date()
	return time.Date(y, m, dd, 0, 0, 0, 0, time.Local)
}

type boardSection struct {
	Section
	Cells []Cell
	// Blanks pads a short section out to a full page of ruled rows. The
	// ground/annex sheet has 10 rooms against the floors' 24, and a half-empty
	// page looks unfinished next to them.
	Blanks []struct{}
}

// BoardCell is one room as the board shows it: the cleaning marker plus
// everything else reception would otherwise have to open another page to see.
type BoardCell struct {
	Cell
	Keycard bool
	Baked   bool
	Notes   []RoomNote
}

type boardSectionUI struct {
	Section
	Cells []BoardCell
}

// printRows is the tallest section, and therefore the row count every printed
// sheet is padded to.
const printRows = 24

func padTo(n int) []struct{} {
	if n >= printRows {
		return nil
	}
	return make([]struct{}, printRows-n)
}

// Total is one line of the daily tally. The three service markers say how much
// linen leaves the store; AN and AN / AF say how many rooms need a full strip
// and turnaround, which is the other half of the day's workload.
type Total struct {
	Marker string
	Class  string
	Count  int
}

func serviceTotals(st *State, d time.Time) []Total {
	counts := map[string]int{}
	for _, r := range AllRooms() {
		counts[Resolve(st, r, d).Marker]++
	}
	out := []Total{}
	for _, m := range []string{
		MarkerFreshen, MarkerTowels, MarkerSheetsTow,
		MarkerDeparture, MarkerTurnover,
	} {
		out = append(out, Total{m, markerClass(m), counts[m]})
	}
	return out
}

func handleBoard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	d := queryDate(r)
	data := map[string]any{
		"Date":     FormatDate(d),
		"DateGr":   FormatGreek(d),
		"CanUndo":  store.CanUndo(),
		"Today":    FormatDate(time.Now()),
		"Tomorrow": FormatDate(time.Now().AddDate(0, 0, 1)),
	}
	store.Read(func(st *State) {
		notes := RoomNotes(st, d)
		date := FormatDate(d)
		secs := []boardSectionUI{}
		for _, s := range Sections {
			bs := boardSectionUI{Section: s}
			for _, c := range SectionCells(st, s, d) {
				kc := NeedsKeycard(c.Marker)
				bs.Cells = append(bs.Cells, BoardCell{
					Cell:    c,
					Keycard: kc,
					Baked:   kc && IsBaked(st, date, c.Room),
					Notes:   notes[c.Room],
				})
			}
			secs = append(secs, bs)
		}
		data["Sections"] = secs
		data["Categories"] = st.Categories
		data["Totals"] = serviceTotals(st, d)
		data["Keycards"] = len(KeycardsFor(st, d))
		data["KeycardTracking"] = st.Settings != nil && st.Settings.KeycardTracking
		data["CustomIcons"] = st.Settings != nil && st.Settings.CustomIcons
	})
	render(w, "board.html", data)
}

// MasterRow is one line of the combined chart: the three sections side by side,
// mirroring the six-column master grid of the original .odt. Shorter sections
// are padded with empty cells so the rows line up.
type MasterRow struct {
	Cells []*Cell
}

func buildMaster(st *State, d time.Time) []MasterRow {
	cols := make([][]Cell, len(Sections))
	max := 0
	for i, s := range Sections {
		cols[i] = SectionCells(st, s, d)
		if len(cols[i]) > max {
			max = len(cols[i])
		}
	}
	rows := make([]MasterRow, max)
	for r := 0; r < max; r++ {
		row := MasterRow{Cells: make([]*Cell, len(cols))}
		for c := range cols {
			if r < len(cols[c]) {
				row.Cells[c] = &cols[c][r]
			}
		}
		rows[r] = row
	}
	return rows
}

func handlePrint(w http.ResponseWriter, r *http.Request) {
	d := queryDate(r)
	data := map[string]any{"DateGr": FormatGreek(d)}
	store.Read(func(st *State) {
		// Each section twice: housekeepers split the floors between
		// themselves, so the copies are deliberately identical.
		secs := []boardSection{}
		for _, s := range Sections {
			cells := SectionCells(st, s, d)
			pad := padTo(len(cells))
			secs = append(secs, boardSection{s, cells, pad})
			secs = append(secs, boardSection{s, cells, pad})
		}
		data["Sections"] = secs
		data["Master"] = buildMaster(st, d)
		data["MasterHeads"] = Sections
		// The keycard sheet leads the run and prints once. Encoding cards is a
		// reception job, not a housekeeping one, so it does not get the second
		// copy the floor sheets do.
		kc := KeycardsFor(st, d)
		data["Keycards"] = kc
		data["KeycardBlanks"] = padTo(len(kc))
	})
	render(w, "print.html", data)
}

func handleCategoriesPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Markers": AllMarkers}
	store.Read(func(st *State) {
		b, _ := json.Marshal(st.Categories)
		data["CategoriesJSON"] = template.JS(b)
		mb, _ := json.Marshal(AllMarkers)
		data["MarkersJSON"] = template.JS(mb)
	})
	render(w, "categories.html", data)
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Println("template:", err)
	}
}

// ---------- API plumbing ----------

func jsonPost(fn func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		out, err := fn(r)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if out == nil {
			out = map[string]any{"ok": true}
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// ---------- API handlers ----------

func apiCheckIn(r *http.Request) (any, error) {
	var req struct {
		Rooms     []string `json:"rooms"`
		Category  string   `json:"category"`
		Arrival   string   `json:"arrival"`
		Departure string   `json:"departure"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if len(req.Rooms) == 0 {
		return nil, fmt.Errorf("no rooms selected")
	}
	return nil, store.Mutate(func(st *State) error {
		for _, room := range req.Rooms {
			s := Stay{Category: req.Category, Arrival: req.Arrival, Departure: req.Departure}
			if err := AddStay(st, room, s); err != nil {
				return err // Mutate rolls back the whole batch
			}
		}
		return nil
	})
}

func apiCheckOut(r *http.Request) (any, error) {
	var req struct {
		Rooms []string `json:"rooms"`
		Date  string   `json:"date"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	d, err := ParseDate(req.Date)
	if err != nil {
		return nil, fmt.Errorf("bad date")
	}
	return nil, store.Mutate(func(st *State) error {
		for _, room := range req.Rooms {
			s := CurrentStay(st, room, d)
			if s == nil {
				return fmt.Errorf("room %s has no stay on %s", room, req.Date)
			}
			a, _ := ParseDate(s.Arrival)
			if !d.After(a) {
				return fmt.Errorf("room %s: cannot depart on the arrival day", room)
			}
			s.Departure = req.Date
		}
		return nil
	})
}

func apiDeleteStay(r *http.Request) (any, error) {
	var req struct {
		Room string `json:"room"`
		ID   string `json:"id"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	return nil, store.Mutate(func(st *State) error {
		list := st.Stays[req.Room]
		for i, s := range list {
			if s.ID == req.ID {
				st.Stays[req.Room] = append(list[:i:i], list[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("stay not found")
	})
}

func apiUndo(r *http.Request) (any, error) { return nil, store.Undo() }

// apiKeycardToggle ticks one keycard off, or un-ticks it.
//
// Deliberately on MutateNoUndo: this is a double-check, and it must not spend
// the single undo step that stands between a mis-selected Check Out and lost
// stay data.
func apiKeycardToggle(r *http.Request) (any, error) {
	var req struct {
		Date string `json:"date"`
		Room string `json:"room"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if _, err := ParseDate(req.Date); err != nil {
		return nil, fmt.Errorf("bad date")
	}
	if SectionOf(req.Room) == "" {
		return nil, fmt.Errorf("unknown room %q", req.Room)
	}
	baked := false
	if err := store.MutateNoUndo(func(st *State) error {
		baked = ToggleBaked(st, req.Date, req.Room)
		return nil
	}); err != nil {
		return nil, err
	}
	// The badge is two different pictures once a custom set is in use, so the
	// server says what to draw rather than the page guessing. Same reasoning as
	// the category preview: one renderer, not two that can disagree.
	name := "keycardred"
	if baked {
		name = "keycardblue"
	}
	return map[string]any{"baked": baked, "icon": string(IconSVG(name))}, nil
}

func apiSettings(r *http.Request) (any, error) {
	var req struct {
		// Pointers so that an absent field leaves that setting alone rather
		// than switching it off.
		KeycardTracking *bool `json:"keycard_tracking"`
		CustomIcons     *bool `json:"custom_icons"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if err := store.MutateNoUndo(func(st *State) error {
		if st.Settings == nil {
			st.Settings = defaultSettings()
		}
		if req.KeycardTracking != nil {
			st.Settings.KeycardTracking = *req.KeycardTracking
		}
		if req.CustomIcons != nil {
			st.Settings.CustomIcons = *req.CustomIcons
		}
		return nil
	}); err != nil {
		return nil, err
	}
	refreshIconsFromStore()
	return nil, nil
}

// ---------- Icon sets ----------

// refreshIconsFromStore re-reads the setting and rescans the directory. Kept
// separate from RefreshIcons so the icon code never has to know about the Store.
func refreshIconsFromStore() {
	custom := false
	store.Read(func(st *State) {
		custom = st.Settings != nil && st.Settings.CustomIcons
	})
	RefreshIcons(custom)
}

type iconSlotView struct {
	Icon
	Have bool
}

func handleIconsPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Err": r.URL.Query().Get("err")}
	store.Read(func(st *State) {
		data["CustomIcons"] = st.Settings != nil && st.Settings.CustomIcons
	})
	have := CustomIconNames()
	slots := []iconSlotView{}
	for _, s := range IconSlots() {
		slots = append(slots, iconSlotView{s, have[s.ID]})
	}
	data["Slots"] = slots
	render(w, "icons.html", data)
}

// handleIconFile serves an uploaded picture. The path has to be exactly a known
// slot plus .png — nothing derived from the request reaches the filesystem
// otherwise, so there is no traversal to defend against.
func handleIconFile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/icons/")
	id := strings.TrimSuffix(name, ".png")
	if id == name || !isIconSlot(id) {
		http.NotFound(w, r)
		return
	}
	// The board is re-rendered as soon as a new picture lands, so a cached copy
	// would show the old one until someone thought to hard-refresh.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(iconsDir(), id+".png"))
}

func iconRedirect(w http.ResponseWriter, r *http.Request, msg string) {
	u := "/icons"
	if msg != "" {
		u += "?err=" + url.QueryEscape(msg)
	}
	http.Redirect(w, r, u, http.StatusSeeOther)
}

// handleIconUpload takes a plain multipart form rather than JSON. It is the one
// place in the app that moves a file, and a form post that lands back on the
// page needs no JavaScript to work at all.
func handleIconUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(maxIconBytes); err != nil {
		iconRedirect(w, r, "the upload was too large or malformed")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		iconRedirect(w, r, "choose a PNG file first")
		return
	}
	defer f.Close()
	// One byte past the limit, so an oversized file is reported as oversized
	// rather than silently truncated to something that still looks like a PNG.
	data, err := io.ReadAll(io.LimitReader(f, maxIconBytes+1))
	if err != nil {
		iconRedirect(w, r, "the file could not be read")
		return
	}
	if err := SaveIcon(r.FormValue("slot"), data); err != nil {
		iconRedirect(w, r, err.Error())
		return
	}
	refreshIconsFromStore()
	iconRedirect(w, r, "")
}

func handleIconRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := RemoveIcon(r.FormValue("slot")); err != nil {
		iconRedirect(w, r, err.Error())
		return
	}
	refreshIconsFromStore()
	iconRedirect(w, r, "")
}

func apiSaveCategories(r *http.Request) (any, error) {
	var req struct {
		Categories []Category `json:"categories"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if len(req.Categories) == 0 {
		return nil, fmt.Errorf("at least one category is required")
	}
	seen := map[string]bool{}
	for _, c := range req.Categories {
		if c.ID == "" || c.Label == "" {
			return nil, fmt.Errorf("every category needs an id and a name")
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("duplicate category id %q", c.ID)
		}
		seen[c.ID] = true
	}
	return nil, store.Mutate(func(st *State) error {
		// A category still attached to a stay cannot vanish.
		for _, existing := range st.Categories {
			if seen[existing.ID] {
				continue
			}
			if rooms := st.CategoryInUse(existing.ID); len(rooms) > 0 {
				return fmt.Errorf("%q is still used by rooms %v — reassign them first",
					existing.Label, rooms)
			}
		}
		st.Categories = req.Categories
		return nil
	})
}

func apiPreview(r *http.Request) (any, error) {
	var req struct {
		Category Category `json:"category"`
		MaxDays  int      `json:"max_days"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.MaxDays <= 0 || req.MaxDays > 60 {
		req.MaxDays = 14
	}
	// Long-stay strip: a hypothetical stay long enough to show the repeat.
	long := PreviewStay(&req.Category, req.MaxDays-1)
	short := map[string][]string{}
	for nights := range req.Category.ShortStay {
		n, err := strconv.Atoi(nights)
		if err != nil || n < 1 || n > 60 {
			continue
		}
		short[nights] = PreviewStay(&req.Category, n)
	}
	return map[string]any{"long": long, "short": short}, nil
}

func apiRoom(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	out := []Stay{}
	labels := map[string]string{}
	store.Read(func(st *State) {
		out = append(out, st.Stays[room]...)
		for _, c := range st.Categories {
			labels[c.ID] = c.Label
		}
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"stays": out, "labels": labels})
}

// ---------- Inventory ----------

func handleInventory(w http.ResponseWriter, r *http.Request) {
	d := queryDate(r)
	data := map[string]any{
		"Date":    FormatDate(d),
		"DateGr":  FormatGreek(d),
		"CanUndo": store.CanUndo(),
	}
	store.Read(func(st *State) {
		data["RoomBoards"] = RoomBoards(st, d)
		data["NamedItems"] = NamedItems(st, d)
		data["Collections"] = Collections(st, d)
		data["Rooms"] = AllRooms()
		b, _ := json.Marshal(st.Items)
		data["ItemsJSON"] = template.JS(b)
		rb, _ := json.Marshal(ReturnRules)
		data["ReturnRulesJSON"] = template.JS(rb)
		labels := map[string]string{}
		for _, rr := range ReturnRules {
			labels[rr] = ReturnRuleLabel(rr)
		}
		lb, _ := json.Marshal(labels)
		data["RuleLabelsJSON"] = template.JS(lb)
		ib, _ := json.Marshal(IconIDs())
		data["IconsJSON"] = template.JS(ib)
	})
	render(w, "inventory.html", data)
}

func handleCollectionPrint(w http.ResponseWriter, r *http.Request) {
	d := queryDate(r)
	data := map[string]any{"DateGr": FormatGreek(d)}
	store.Read(func(st *State) {
		items := Collections(st, d)
		data["Items"] = items
		data["Blanks"] = padTo(len(items))
	})
	render(w, "collect.html", data)
}

// apiLend takes a list of (item, room) pairs. Per-room items are lent by
// selecting rooms on a grid; named items by selecting the item and choosing a
// room. Both arrive here in the same shape.
func apiLend(r *http.Request) (any, error) {
	var req struct {
		Loans []struct {
			Item string `json:"item"`
			Room string `json:"room"`
		} `json:"loans"`
		LentOn string `json:"lent_on"`
		DueOn  string `json:"due_on"`
		Note   string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if len(req.Loans) == 0 {
		return nil, fmt.Errorf("nothing selected")
	}
	return nil, store.Mutate(func(st *State) error {
		for _, x := range req.Loans {
			if err := LendItem(st, x.Item, x.Room, req.LentOn, req.DueOn, req.Note); err != nil {
				return err
			}
		}
		return nil
	})
}

func apiReturn(r *http.Request) (any, error) {
	var req struct {
		IDs  []string `json:"ids"`
		Date string   `json:"date"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if len(req.IDs) == 0 {
		return nil, fmt.Errorf("nothing selected")
	}
	return nil, store.Mutate(func(st *State) error {
		for _, id := range req.IDs {
			if err := ReturnLoan(st, id, req.Date); err != nil {
				return err
			}
		}
		return nil
	})
}

func apiSaveItems(r *http.Request) (any, error) {
	var req struct {
		Items []Item `json:"items"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, t := range req.Items {
		if t.ID == "" || t.Label == "" {
			return nil, fmt.Errorf("every item needs an id and a name")
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("duplicate item id %q", t.ID)
		}
		seen[t.ID] = true
	}
	// An item saved without an icon gets one guessed from its name, so adding
	// "Σίδερο 5" does not also require picking a picture of an iron.
	for i := range req.Items {
		if req.Items[i].Icon == "" {
			req.Items[i].Icon = guessIcon(req.Items[i].Label)
		}
	}
	return nil, store.Mutate(func(st *State) error {
		// An item still out on loan cannot vanish, or the loan becomes
		// unreturnable.
		for _, existing := range st.Items {
			if seen[existing.ID] {
				continue
			}
			if rooms := st.ItemInUse(existing.ID); len(rooms) > 0 {
				return fmt.Errorf("%q is still out to rooms %v — collect it first",
					existing.Label, rooms)
			}
		}
		st.Items = req.Items
		return nil
	})
}
