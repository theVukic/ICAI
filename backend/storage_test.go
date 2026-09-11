package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
)

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

func TestExportZIPKeepsProcessedCoverOnlyInsideZIP(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.jpg")
	processed := filepath.Join(dir, "processed.png")
	if err := os.WriteFile(original, []byte("original-image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(processed, []byte("generated-cover"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := &CatalogStore{
		catalogs: []UploadedCatalog{{Items: []CatalogItem{{
			ItemID: "item-1", TowKod: "ABC-1", Images: []CatalogImage{{Path: original}},
			TechnicalDescription: "<p>AI opis</p>", OriginalTechnicalDescription: "Početni opis", TechnicalDescriptionFormat: "html",
		}}}},
		pendingPublishes: map[string]CatalogItem{},
		categorizations:  map[string]ItemCategorySelection{},
		processedImages:  map[string]string{"item-1": processed},
		processingItems:  map[string]ImageProcessingItem{"item-1": {Status: "done"}},
	}

	var output bytes.Buffer
	if _, err := store.WriteExportZIP(&output, nil, nil); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}

	var payload CatalogPayload
	entries := map[string]bool{}
	for _, entry := range reader.File {
		entries[entry.Name] = true
		if entry.Name != "catalog.json" {
			continue
		}
		input, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		decodeErr := json.NewDecoder(input).Decode(&payload)
		_ = input.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
	}
	if len(payload.Items) != 1 || payload.Items[0].ProcessedCover != "processed/ABC-1/cover.png" {
		t.Fatalf("processed cover was not recorded in catalog ZIP: %#v", payload.Items)
	}
	if !entries[payload.Items[0].ProcessedCover] {
		t.Fatalf("processed cover entry %q missing from ZIP", payload.Items[0].ProcessedCover)
	}

	zipPath := filepath.Join(dir, "saved.zip")
	if err := os.WriteFile(zipPath, output.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	importTemp := filepath.Join(dir, "import")
	imported, _, err := extractCatalogZip(zipPath, importTemp, "saved.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Items) != 1 || imported.Items[0].ProcessedCover == "" {
		t.Fatalf("processed cover was not restored from ZIP: %#v", imported.Items)
	}
	if _, err := os.Stat(imported.Items[0].ProcessedCover); err != nil {
		t.Fatalf("restored processed cover does not exist: %v", err)
	}

	restoredStore, err := NewCatalogStore()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(restoredStore.TempDir())
	input, err := os.Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := restoredStore.AddCatalog(input, &multipart.FileHeader{Filename: "saved.zip", Size: info.Size()})
	if err != nil {
		t.Fatal(err)
	}
	coverPath, progress := restoredStore.ProcessedImage(catalog.Items[0].ItemID)
	if progress.Status != "done" || coverPath == "" {
		t.Fatalf("saved ZIP cover should be immediately reusable, got %q / %#v", coverPath, progress)
	}
	descriptionProgress := restoredStore.DescriptionProcessing(catalog.Items[0].ItemID)
	if descriptionProgress.Status != "done" {
		t.Fatalf("saved ZIP description should be immediately reusable, got %#v", descriptionProgress)
	}
}

func TestAIWorkBlockingUsesOnlyRequestedItems(t *testing.T) {
	store := &CatalogStore{
		catalogs: []UploadedCatalog{{Items: []CatalogItem{{ItemID: "ready", TechnicalDescriptionFormat: "html"}, {ItemID: "pending"}, {ItemID: "failed"}}}},
		descriptionItems: map[string]DescriptionProcessingItem{
			"ready": {Status: "done"}, "pending": {Status: "processing"}, "failed": {Status: "error"},
		},
	}
	if pending, failed := store.AIWorkBlocking([]string{"ready"}, nil); pending != 0 || failed != 0 {
		t.Fatalf("ready selection should not block, pending=%d failed=%d", pending, failed)
	}
	if pending, failed := store.AIWorkBlocking(nil, nil); pending != 1 || failed != 0 {
		t.Fatalf("only active AI work should block, pending=%d failed=%d", pending, failed)
	}
}

func TestAIWorkBlockingDoesNotBlockAnUnrequestedDescription(t *testing.T) {
	store := &CatalogStore{
		catalogs:         []UploadedCatalog{{Items: []CatalogItem{{ItemID: "item", TechnicalDescription: "Početni opis", TechnicalDescriptionFormat: "plain"}}}},
		descriptionItems: map[string]DescriptionProcessingItem{"item": {Status: "done"}},
	}
	if pending, failed := store.AIWorkBlocking(nil, nil); pending != 0 || failed != 0 {
		t.Fatalf("an unrequested AI description must not block, pending=%d failed=%d", pending, failed)
	}
}

func TestChangingNameOrPriceDoesNotCompleteAnUnchangedAIDescription(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	item := CatalogItem{ItemID: "item-1", NameB2B: "Stari naziv", RetailPrice: 10, TechnicalDescription: "Početni opis", TechnicalDescriptionFormat: "plain"}
	payload, err := json.Marshal(CatalogPayload{Items: []CatalogItem{item}})
	if err != nil || os.WriteFile(catalogPath, payload, 0o644) != nil {
		t.Fatal("failed to prepare catalog")
	}
	store := &CatalogStore{
		catalogs:         []UploadedCatalog{{SavedPath: catalogPath, Items: []CatalogItem{item}}},
		descriptionItems: map[string]DescriptionProcessingItem{"item-1": {Status: "processing"}},
	}
	updated, err := store.UpdateItemByID("item-1", CatalogItemEditRequest{Name: "Novi naziv", Price: 12, Description: "Početni opis"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NameB2B != "Novi naziv" || updated.RetailPrice != 12 {
		t.Fatalf("name and price were not updated: %#v", updated)
	}
	if updated.TechnicalDescription != "Početni opis" || updated.TechnicalDescriptionFormat != "plain" {
		t.Fatalf("unchanged description was altered: %#v", updated)
	}
	if status := store.DescriptionProcessing("item-1"); status.Status != "processing" {
		t.Fatalf("AI work must continue after name/price edit, got %#v", status)
	}
}

func TestTemplateKeepsIndexOutsideAIHTML(t *testing.T) {
	item := CatalogItem{Index: "0949", TechnicalDescription: "<p>AI opis</p>", TechnicalDescriptionFormat: "html"}
	if got := renderDescriptionTemplate("${technicalDescription} ${index}", item); got != "<p>AI opis</p> 0949" {
		t.Fatalf("template must keep index outside AI description, got %q", got)
	}
	item.TechnicalDescriptionFormat = "plain"
	if got := renderDescriptionTemplate("${index}", item); got != "0949" {
		t.Fatalf("standard template must preserve index, got %q", got)
	}
}
