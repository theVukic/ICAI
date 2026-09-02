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
}
