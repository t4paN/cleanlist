package main

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Icons are inline SVG rather than emoji or an icon font.
//
// Emoji render as colour bitmaps that differ between machines and print badly;
// an icon font would be a dependency and another file to embed. These are plain
// stroked paths on a 24x24 grid using currentColor, so they inherit the text
// colour, stay monochrome on the printed sheet, and scale cleanly.

type Icon struct {
	ID   string
	Name string
	Path string
}

var iconSet = []Icon{
	{"key", "Key", `<path d="M14.5 5.5a4 4 0 1 1-3.2 6.4L4 19.2V21h2.8l1-1v-1.6h1.6l1-1v-1.6h1.6l1.5-1.5a4 4 0 0 0 4-8zM16 8.6h.01"/>`},
	{"iron", "Iron", `<path d="M3 17h16a2 2 0 0 0 2-2 7 7 0 0 0-7-7H8a5 5 0 0 0-5 5zM3 20h16"/><path d="M12 8V6a2 2 0 0 1 2-2h3"/>`},
	{"hairdryer", "Hairdryer", `<path d="M4 10a5 5 0 0 1 5-5h6a5 5 0 0 1 0 10H9a5 5 0 0 1-5-5z"/><path d="M9 15v4a2 2 0 0 0 2 2h1"/><path d="M15 8v4"/>`},
	{"remote", "Remote", `<rect x="8" y="2" width="8" height="20" rx="2"/><path d="M11 6h2M10 11h.01M14 11h.01M10 15h.01M14 15h.01"/>`},
	{"towel", "Towel", `<path d="M6 3h12v16a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2z"/><path d="M6 7h12M9 3v18"/>`},
	{"plug", "Charger", `<path d="M9 2v6M15 2v6"/><path d="M6 8h12v3a6 6 0 0 1-6 6 6 6 0 0 1-6-6z"/><path d="M12 17v5"/>`},
	{"umbrella", "Umbrella", `<path d="M12 2a9 9 0 0 1 9 9H3a9 9 0 0 1 9-9z"/><path d="M12 11v8a2 2 0 0 1-4 0"/>`},
	{"crib", "Cot", `<path d="M3 6v14M21 6v14M3 20h18M3 10h18"/><path d="M7 6v4M11 6v4M15 6v4M19 6v4"/>`},
	{"kettle", "Kettle", `<path d="M6 9h11l3-3v5l-3 2v6a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1z"/><path d="M9 9V7a3 3 0 0 1 6 0v2"/>`},
	{"box", "Other", `<path d="M3 8l9-5 9 5v8l-9 5-9-5z"/><path d="M3 8l9 5 9-5M12 13v8"/>`},
}

var iconByID = func() map[string]Icon {
	m := map[string]Icon{}
	for _, i := range iconSet {
		m[i.ID] = i
	}
	return m
}()

// ---------- Custom icon sets ----------
//
// A hotel can replace the drawn icons with pictures of its own. They live in an
// icons/ directory beside the executable rather than inside
// cleanlist-data.json.
//
// That placement is the whole point. The data file is the only copy of live
// occupancy, a corrupt one deliberately stops the app starting, and thirty
// daily backups would carry every uploaded picture forever. Out here a missing
// or broken picture is just a missing picture: it falls back to the drawing and
// nobody loses a booking over it.

// keycardSlots are upload targets with no item of their own. They fall back to
// the key drawing, which the board then tints red or blue itself — so leaving
// them empty costs nothing.
func keycardSlots() []Icon {
	key := iconByID["key"].Path
	return []Icon{
		{"keycardred", "Keycard — to make", key},
		{"keycardblue", "Keycard — made", key},
	}
}

// IconSlots lists every name a custom icon may be uploaded under.
//
// Uploads are filed under a slot chosen from this list, never under the name
// the browser sent. That is what makes a path traversal impossible rather than
// merely unlikely, and it is why the list is fixed in code.
func IconSlots() []Icon {
	out := make([]Icon, 0, len(iconSet)+2)
	out = append(out, iconSet...)
	out = append(out, keycardSlots()...)
	return out
}

func isIconSlot(id string) bool {
	for _, s := range IconSlots() {
		if s.ID == id {
			return true
		}
	}
	return false
}

func lookupIcon(id string) (Icon, bool) {
	if ic, ok := iconByID[id]; ok {
		return ic, true
	}
	for _, s := range keycardSlots() {
		if s.ID == id {
			return s, true
		}
	}
	return Icon{}, false
}

func iconsDir() string { return filepath.Join(dataDir(), "icons") }

// iconMode caches everything the renderer needs.
//
// Templates execute after the Store's lock has been released, and a helper that
// reached back into the Store would deadlock the first time anyone moved a
// render call inside a Read. Its own small lock keeps that impossible.
var iconMode struct {
	mu     sync.RWMutex
	custom bool
	have   map[string]bool
}

// RefreshIcons rescans the directory and records whether custom icons are on.
// Called at startup and after any upload, removal or settings change.
func RefreshIcons(custom bool) {
	have := map[string]bool{}
	for _, s := range IconSlots() {
		fi, err := os.Stat(filepath.Join(iconsDir(), s.ID+".png"))
		if err == nil && fi.Mode().IsRegular() {
			have[s.ID] = true
		}
	}
	iconMode.mu.Lock()
	iconMode.custom = custom
	iconMode.have = have
	iconMode.mu.Unlock()
}

// CustomIconNames reports which slots currently have a file, for the editor.
func CustomIconNames() map[string]bool {
	iconMode.mu.RLock()
	defer iconMode.mu.RUnlock()
	out := make(map[string]bool, len(iconMode.have))
	for k, v := range iconMode.have {
		out[k] = v
	}
	return out
}

const maxIconBytes = 512 << 10

var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// SaveIcon writes an uploaded picture into the icons directory.
//
// The filename comes from the slot and the bytes have to actually begin like a
// PNG. Both checks are cheap, and both remove a class of problem outright
// rather than mitigating it.
func SaveIcon(slot string, data []byte) error {
	if !isIconSlot(slot) {
		return fmt.Errorf("unknown icon %q", slot)
	}
	if len(data) == 0 {
		return fmt.Errorf("the file is empty")
	}
	if len(data) > maxIconBytes {
		return fmt.Errorf("the icon is %dKB and the limit is %dKB",
			len(data)>>10, maxIconBytes>>10)
	}
	if !bytes.HasPrefix(data, pngMagic) {
		return fmt.Errorf("only PNG files are accepted")
	}
	if err := os.MkdirAll(iconsDir(), 0o755); err != nil {
		return err
	}
	// Written the same way as the data file: temp then rename, so a half
	// received upload is never what the board picks up.
	path := filepath.Join(iconsDir(), slot+".png")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RemoveIcon drops a custom picture, returning that slot to its drawing.
func RemoveIcon(slot string) error {
	if !isIconSlot(slot) {
		return fmt.Errorf("unknown icon %q", slot)
	}
	if err := os.Remove(filepath.Join(iconsDir(), slot+".png")); err != nil &&
		!os.IsNotExist(err) {
		return err
	}
	return nil
}

// IconSVG renders an icon by id. An unknown or empty id yields nothing rather
// than a placeholder, so an item with no icon simply shows its name.
func IconSVG(id string) template.HTML {
	iconMode.mu.RLock()
	custom, have := iconMode.custom, iconMode.have[id]
	iconMode.mu.RUnlock()

	// have is only ever set for names taken from the slot allowlist, so the id
	// can go straight into the URL.
	if custom && have {
		return template.HTML(`<img class="icon" src="/icons/` + id + `.png" alt="">`)
	}

	ic, ok := lookupIcon(id)
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="icon" viewBox="0 0 24 24" fill="none" ` +
		`stroke="currentColor" stroke-width="1.6" stroke-linecap="round" ` +
		`stroke-linejoin="round" aria-hidden="true">` + ic.Path + `</svg>`)
}

// IconIDs lists the available icons for the editor, in display order.
func IconIDs() []Icon {
	out := make([]Icon, len(iconSet))
	copy(out, iconSet)
	return out
}

// guessIcon picks a sensible default from an item's name, so adding "Σίδερο 5"
// does not require also choosing a picture of an iron.
func guessIcon(label string) string {
	l := []rune(lowerGreek(label))
	has := func(sub string) bool { return containsRunes(l, []rune(sub)) }
	switch {
	case has("κλειδ") || has("key"):
		return "key"
	case has("σιδερ") || has("iron"):
		return "iron"
	case has("σεσουαρ") || has("hair"):
		return "hairdryer"
	case has("τηλεχειρ") || has("remote"):
		return "remote"
	case has("πετσετ") || has("towel"):
		return "towel"
	case has("φορτιστ") || has("charger") || has("adapt"):
		return "plug"
	case has("ομπρελ") || has("umbrella"):
		return "umbrella"
	case has("κουνια") || has("crib") || has("cot"):
		return "crib"
	case has("βραστηρ") || has("kettle"):
		return "kettle"
	}
	return "box"
}

// lowerGreek lowercases and strips accents so "Σίδερο" matches "σιδερ".
func lowerGreek(s string) string {
	repl := map[rune]rune{
		'ά': 'α', 'έ': 'ε', 'ή': 'η', 'ί': 'ι', 'ϊ': 'ι', 'ΐ': 'ι',
		'ό': 'ο', 'ύ': 'υ', 'ϋ': 'υ', 'ΰ': 'υ', 'ώ': 'ω', 'ς': 'σ',
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r += 32
		}
		if r >= 'Α' && r <= 'Ω' {
			r += 32
		}
		if x, ok := repl[r]; ok {
			r = x
		}
		out = append(out, r)
	}
	return string(out)
}

func containsRunes(hay, needle []rune) bool {
	if len(needle) == 0 || len(needle) > len(hay) {
		return len(needle) == 0
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// sortedIconIDs is used by tests to assert the set is stable.
func sortedIconIDs() []string {
	out := []string{}
	for _, i := range iconSet {
		out = append(out, i.ID)
	}
	sort.Strings(out)
	return out
}
