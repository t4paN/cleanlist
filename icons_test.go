package main

import (
	"strings"
	"testing"
)

// The slot list is the security boundary: an upload is filed under a name from
// this list, never under the name the browser sent.
func TestIconSlotAllowlist(t *testing.T) {
	for _, id := range []string{"key", "iron", "box", "keycardred", "keycardblue"} {
		if !isIconSlot(id) {
			t.Fatalf("%q should be a slot", id)
		}
	}
	for _, id := range []string{
		"", "safekey", "iron.png", "../iron", "../../etc/passwd",
		"/etc/passwd", "iron/../../x", "KEY",
	} {
		if isIconSlot(id) {
			t.Fatalf("%q must not be a slot", id)
		}
	}
}

// Every drawn icon can be replaced, and the two keycard states are slots of
// their own even though neither has an item.
func TestIconSlotsCoverDrawingsAndKeycards(t *testing.T) {
	slots := map[string]bool{}
	for _, s := range IconSlots() {
		slots[s.ID] = true
	}
	for _, ic := range iconSet {
		if !slots[ic.ID] {
			t.Fatalf("drawn icon %q has no upload slot", ic.ID)
		}
	}
	if !slots["keycardred"] || !slots["keycardblue"] {
		t.Fatal("both keycard states need a slot")
	}
}

func TestSaveIconRejectsUnknownSlot(t *testing.T) {
	png := append([]byte{}, pngMagic...)
	for _, slot := range []string{"../evil", "safekey", ""} {
		if err := SaveIcon(slot, png); err == nil {
			t.Fatalf("SaveIcon(%q) should have been refused", slot)
		}
	}
}

// Extension is not evidence. The bytes have to start like a PNG.
func TestSaveIconRejectsNonPNG(t *testing.T) {
	err := SaveIcon("iron", []byte("<?php echo 1; ?>"))
	if err == nil {
		t.Fatal("a non-PNG upload should have been refused")
	}
	if !strings.Contains(err.Error(), "PNG") {
		t.Fatalf("error %q should say what was wrong", err)
	}
}

func TestSaveIconRejectsEmptyAndOversize(t *testing.T) {
	if err := SaveIcon("iron", nil); err == nil {
		t.Fatal("an empty upload should have been refused")
	}
	big := append(append([]byte{}, pngMagic...), make([]byte, maxIconBytes)...)
	if err := SaveIcon("iron", big); err == nil {
		t.Fatal("an oversized upload should have been refused")
	}
}

// Nothing is uploaded during a test run, so every slot falls back to its
// drawing. keycardred and keycardblue borrow the key drawing, which the board
// then tints itself.
func TestIconSVGFallsBackToDrawings(t *testing.T) {
	for _, id := range []string{"key", "iron", "keycardred", "keycardblue"} {
		got := string(IconSVG(id))
		if !strings.HasPrefix(got, "<svg") {
			t.Fatalf("IconSVG(%q) = %q, want the drawing", id, got)
		}
	}
	if got := IconSVG("nope"); got != "" {
		t.Fatalf("an unknown icon should render nothing, got %q", got)
	}
}
