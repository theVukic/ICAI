package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestTitleHistoryPersistsAndRanksSuggestions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "title-history.json")
	store := NewTitleHistoryStore(path)
	for _, title := range []string{
		"Audi A4 prednji branik",
		"Branik prednji Audi A4",
		"Audi A4 zadnji branik",
		"Audi A6 prednji branik",
	} {
		if err := store.Remember(title); err != nil {
			t.Fatal(err)
		}
	}
	// A repeated title should win a tie between equally strong prefixes.
	if err := store.Remember("Audi A4 zadnji branik"); err != nil {
		t.Fatal(err)
	}

	reloaded := NewTitleHistoryStore(path)
	got := reloaded.Suggestions("audi a4")
	if len(got) != 3 {
		t.Fatalf("expected 3 suggestions, got %#v", got)
	}
	if got[0] != "Audi A4 zadnji branik" {
		t.Fatalf("expected frequently used title first, got %#v", got)
	}
}

func TestTitleHistoryMatchesBosnianLettersWithoutDiacritics(t *testing.T) {
	store := NewTitleHistoryStore(filepath.Join(t.TempDir(), "title-history.json"))
	if err := store.Remember("Stražnji nosač branika"); err != nil {
		t.Fatal(err)
	}
	got := store.Suggestions("straznji nosac")
	if len(got) != 1 || got[0] != "Stražnji nosač branika" {
		t.Fatalf("unexpected suggestions: %#v", got)
	}
}

func TestTitleHistoryReturnsEveryMatchingTitleAndIgnoresCase(t *testing.T) {
	store := NewTitleHistoryStore(filepath.Join(t.TempDir(), "title-history.json"))
	for index := 1; index <= 25; index++ {
		title := fmt.Sprintf("GUMENE PATOSNICE | BMW X%d", index)
		if err := store.Remember(title); err != nil {
			t.Fatal(err)
		}
	}

	got := store.Suggestions("gumene patosnice")
	if len(got) != 25 {
		t.Fatalf("expected every matching title, got %d", len(got))
	}
}

func TestTitleHistoryDeleteIsCaseInsensitiveAndPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "title-history.json")
	store := NewTitleHistoryStore(path)
	if err := store.Remember("GUMENE PATOSNICE | BMW X5"); err != nil {
		t.Fatal(err)
	}

	deleted, err := store.Delete("gumene patosnice | bmw x5")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("expected title to be deleted")
	}
	if got := NewTitleHistoryStore(path).Suggestions("gumene"); len(got) != 0 {
		t.Fatalf("deleted title was loaded again: %#v", got)
	}
}
