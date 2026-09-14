package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCollectUnpublishedSelectionKeysKeepsFailedItemWithSharedTowKod(t *testing.T) {
	items := []CatalogItem{
		{TowKod: "RB-010", SelectionKey: "catalog-1|RB-010|1|first"},
		{TowKod: "RB-010", SelectionKey: "catalog-1|RB-010|2|second"},
	}

	remaining := collectUnpublishedSelectionKeys(items, []string{items[0].SelectionKey})
	if len(remaining) != 1 || remaining[0] != items[1].SelectionKey {
		t.Fatalf("expected only the unpublished item to remain reserved, got %#v", remaining)
	}
}

func TestBuildListingPayloadDisablesImmediateAvailability(t *testing.T) {
	const categoryID = 123
	ready := make(chan struct{})
	close(ready)

	app := &App{olxCategories: &OLXCategoryService{}}
	attributeCache := &categoryAttributesCache{entries: map[int]*categoryAttributesCacheEntry{
		categoryID: {ready: ready, attributes: []OLXCategoryAttribute{}},
	}}
	item := CatalogItem{
		NameB2B:     "Testni proizvod",
		RetailPrice: 25,
		Categorization: &ItemCategorySelection{
			Category:   &OLXCategoryNode{ID: categoryID},
			Attributes: map[string]string{},
		},
	}

	payload, err := app.buildListingPayload(context.Background(), item, DescriptionDocument{}, attributeCache)
	if err != nil {
		t.Fatalf("build listing payload: %v", err)
	}
	if payload.Available {
		t.Fatal("expected immediate availability to be disabled")
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal listing payload: %v", err)
	}
	if !strings.Contains(string(encoded), `"available":false`) {
		t.Fatalf("expected OLX payload to explicitly include available=false, got %s", encoded)
	}
}

func TestMakeDescriptionOLXCompatibleKeepsVisibleBulletMarkers(t *testing.T) {
	input := `<p><strong>Karakteristike:</strong></p><ul><li><strong>Aluminijumski nosač</strong>. Set 2 šipke.</li><li>Bravica.</li></ul>`
	got := makeDescriptionOLXCompatible(input)
	want := `<p><strong>Karakteristike:</strong></p><p>• <strong>Aluminijumski nosač</strong>. Set 2 šipke.</p><p>• Bravica.</p>`
	if got != want {
		t.Fatalf("unexpected OLX description:\nwant: %q\n got: %q", want, got)
	}
}

func TestMakeDescriptionOLXCompatibleUnderstandsQuillLists(t *testing.T) {
	input := `<ol><li data-list="bullet"><span class="ql-ui" contenteditable="false"></span>Bullet</li><li data-list="ordered"><span class="ql-ui" contenteditable="false"></span>Number</li></ol>`
	got := makeDescriptionOLXCompatible(input)
	want := `<p>• Bullet</p><p>1. Number</p>`
	if got != want {
		t.Fatalf("unexpected Quill list conversion:\nwant: %q\n got: %q", want, got)
	}
}

func TestMakeDescriptionOLXCompatiblePreservesEmptyEditorLines(t *testing.T) {
	input := `<p>Prvi red</p><p></p><p><br></p><p>Drugi red</p>`
	want := `<p>Prvi red</p><br><br><p>Drugi red</p>`
	if got := makeDescriptionOLXCompatible(input); got != want {
		t.Fatalf("empty editor lines were not preserved:\nwant: %q\n got: %q", want, got)
	}
}
