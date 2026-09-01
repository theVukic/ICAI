package main

import "testing"

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
