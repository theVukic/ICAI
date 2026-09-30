package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcessedImageCacheReusesOnlyMatchingSourceImage(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.jpg")
	processed := filepath.Join(dir, "generated.png")
	if err := os.WriteFile(source, []byte("original-v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(processed, []byte("generated-cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache, err := NewProcessedImageCache(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	item := CatalogItem{ItemID: "first-import", TowKod: "ABC-1", Images: []CatalogImage{{ID: "supplier", Source: "supplier", AIInput: true, Path: source}}}
	if err := cache.Save(item, processed); err != nil {
		t.Fatal(err)
	}
	reimported := item
	reimported.ItemID = "next-import"
	if _, found := cache.Lookup(reimported); !found {
		t.Fatal("same source image should reuse the saved generated cover")
	}
	if err := os.WriteFile(source, []byte("original-v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, found := cache.Lookup(reimported); found {
		t.Fatal("changed source image must be processed again")
	}
}
