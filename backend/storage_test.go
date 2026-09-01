package main

import "testing"

func TestBuildStateKeepsEveryItemWithSharedSupplierFields(t *testing.T) {
	store := &CatalogStore{
		catalogs: []UploadedCatalog{{
			ID: "catalog-1",
			Items: []CatalogItem{
				{ItemID: "item-a", TowKod: "AS-100", Index: "1", NameB2B: "Krovni nosač 120 cm"},
				{ItemID: "item-b", TowKod: "AS-100", Index: "1", NameB2B: "Krovni nosač 120 cm"},
				{ItemID: "item-c", TowKod: "AS-100", Index: "1", NameB2B: "Krovni nosač 120 cm"},
			},
		}},
		pendingPublishes: map[string]CatalogItem{},
		categorizations:  map[string]ItemCategorySelection{},
	}

	state := store.buildStateLocked()
	if len(state.Items) != 3 {
		t.Fatalf("expected every imported row to remain visible, got %#v", state.Items)
	}
	if state.Items[0].SelectionKey == state.Items[1].SelectionKey || state.Items[1].SelectionKey == state.Items[2].SelectionKey {
		t.Fatalf("expected unique selection keys for products with identical supplier fields")
	}
}
