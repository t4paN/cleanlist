package main

import (
	"html/template"
	"sort"
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

// IconSVG renders an icon by id. An unknown or empty id yields nothing rather
// than a placeholder, so an item with no icon simply shows its name.
func IconSVG(id string) template.HTML {
	ic, ok := iconByID[id]
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
