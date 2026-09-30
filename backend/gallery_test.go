package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceCatalogImagesReordersDeletesAndSkipsDuplicate(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	item := CatalogItem{ItemID: "item-1", Images: []CatalogImage{
		{ID: "supplier", Position: 0, URL: "https://example.test/one.jpg", Source: "supplier", Checksum: "same"},
		{ID: "ai", Position: 1, Path: filepath.Join(dir, "ai.png"), Source: "ai", Checksum: "ai-hash"},
	}}
	writeTestCatalog(t, catalogPath, item)
	store := &CatalogStore{
		catalogs:        []UploadedCatalog{{SavedPath: catalogPath, Items: []CatalogItem{item}}},
		processedImages: map[string]string{"item-1": item.Images[1].Path},
		processingItems: map[string]ImageProcessingItem{"item-1": {Status: "done"}},
	}

	updated, err := store.ReplaceCatalogImages("item-1", []string{"new:0", "ai"}, []CatalogImage{{
		ID: "duplicate-upload", Source: "manual", Checksum: "same", Path: filepath.Join(dir, "manual.jpg"),
	}}, []string{"supplier", "ai"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Images) != 2 || updated.Images[0].ID != "supplier" || updated.Images[0].Position != 0 {
		t.Fatalf("duplicate/reorder was not normalized: %#v", updated.Images)
	}
	if updated.Images[1].ID != "ai" || updated.Images[1].Position != 1 {
		t.Fatalf("AI image order was not preserved: %#v", updated.Images)
	}

	updated, err = store.ReplaceCatalogImages("item-1", []string{}, nil, []string{"supplier", "ai"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Images) != 0 {
		t.Fatalf("expected the item to be allowed without images: %#v", updated.Images)
	}
	if _, ok := store.processingItems["item-1"]; ok {
		t.Fatal("AI status remained after its image was deleted")
	}
}

func TestAddCatalogImagesBulkMakesNewCoverAndKeepsOldCoverSecond(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	first := CatalogImage{ID: "old-cover", Position: 0, URL: "https://example.test/old.jpg", Source: "supplier"}
	items := []CatalogItem{{ItemID: "item-1", Images: []CatalogImage{first}}, {ItemID: "item-2", Images: []CatalogImage{first}}}
	payload, err := json.Marshal(CatalogPayload{Items: items})
	if err != nil || os.WriteFile(catalogPath, payload, 0o644) != nil {
		t.Fatal("failed to prepare catalog")
	}
	store := &CatalogStore{catalogs: []UploadedCatalog{{SavedPath: catalogPath, Items: items}}}
	upload := CatalogImage{ID: "upload", Path: filepath.Join(dir, "cover.webp"), Source: "manual", Checksum: "cover-hash"}

	updated, skipped, err := store.AddCatalogImagesBulk([]string{"item-1", "item-2"}, []CatalogImage{upload}, true)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 2 || skipped != 0 {
		t.Fatalf("unexpected bulk result updated=%d skipped=%d", updated, skipped)
	}
	for _, item := range store.catalogs[0].Items {
		if len(item.Images) != 2 || item.Images[0].Checksum != "cover-hash" || item.Images[1].ID != "old-cover" {
			t.Fatalf("old cover did not remain second: %#v", item.Images)
		}
		if item.Images[0].ID == "upload" {
			t.Fatal("bulk image ID was not made item-specific")
		}
	}
}

func TestManualGallerySaveExportsAndPreparesEveryImageForPublish(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		writeTestPNG(t, dir, "supplier.png", color.RGBA{R: 220, A: 255}),
		writeTestPNG(t, dir, "manual-one.png", color.RGBA{G: 220, A: 255}),
		writeTestPNG(t, dir, "manual-two.png", color.RGBA{B: 220, A: 255}),
	}
	catalogPath := filepath.Join(dir, "catalog.json")
	item := CatalogItem{ItemID: "item-1", TowKod: "TEST-1", Images: []CatalogImage{{
		ID: "supplier", Position: 0, Path: paths[0], Source: "supplier", Checksum: fileChecksum(paths[0]), AIInput: true,
	}}}
	writeTestCatalog(t, catalogPath, item)
	store := &CatalogStore{
		catalogs:         []UploadedCatalog{{SavedPath: catalogPath, Items: []CatalogItem{item}}},
		pendingPublishes: map[string]CatalogItem{},
		categorizations:  map[string]ItemCategorySelection{},
	}

	// This is the same final operation used by the gallery's SAČUVAJ button:
	// the second manual image becomes cover, the supplier image remains second.
	updated, err := store.ReplaceCatalogImages("item-1", []string{"new:1", "supplier", "new:0"}, []CatalogImage{
		{ID: "manual-one", Path: paths[1], Source: "manual", Checksum: fileChecksum(paths[1])},
		{ID: "manual-two", Path: paths[2], Source: "manual", Checksum: fileChecksum(paths[2])},
	}, []string{"supplier"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Images) != 3 || updated.Images[0].ID != "manual-two" || updated.Images[1].ID != "supplier" || updated.Images[2].ID != "manual-one" {
		t.Fatalf("gallery save did not preserve all images and order: %#v", updated.Images)
	}

	var archive bytes.Buffer
	if _, err := store.WriteExportZIP(&archive, []string{"item-1"}, nil); err != nil {
		t.Fatal(err)
	}
	zipReader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var exported CatalogPayload
	entries := make(map[string]bool)
	for _, entry := range zipReader.File {
		entries[entry.Name] = true
		if entry.Name != "catalog.json" {
			continue
		}
		file, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		decodeErr := json.NewDecoder(file).Decode(&exported)
		_ = file.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
	}
	if len(exported.Items) != 1 || len(exported.Items[0].Images) != 3 {
		t.Fatalf("ZIP did not contain every gallery image: %#v", exported.Items)
	}
	for index, exportedImage := range exported.Items[0].Images {
		if exportedImage.Position != index || !entries[exportedImage.Path] {
			t.Fatalf("ZIP image %d is missing or out of order: %#v", index, exportedImage)
		}
	}
	roundTripZIP := filepath.Join(dir, "round-trip.zip")
	if err := os.WriteFile(roundTripZIP, archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	roundTrip, _, err := extractCatalogZip(roundTripZIP, filepath.Join(dir, "round-trip"), "round-trip.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(roundTrip.Items) != 1 || len(roundTrip.Items[0].Images) != 3 {
		t.Fatalf("ZIP round-trip lost gallery images: %#v", roundTrip.Items)
	}
	if roundTrip.Items[0].Images[1].ID != "supplier" || !roundTrip.Items[0].Images[1].AIInput {
		t.Fatalf("ZIP round-trip lost original AI supplier marker: %#v", roundTrip.Items[0].Images)
	}

	// Exercise the exact local multipart preparation used before the OLX HTTP
	// request. No request is sent; the generated multipart body is parsed here.
	ordered := orderedCatalogImages(updated.Images)
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	boundary := multipartWriter.Boundary()
	streamErr := make(chan error, 1)
	service := &OLXCategoryService{}
	go func() {
		streamErr <- service.streamImagesToMultipart(context.Background(), multipartWriter, pipeWriter, ordered)
	}()
	body, readErr := io.ReadAll(pipeReader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := <-streamErr; err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	uploadCount := 0
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		data, partErr := io.ReadAll(part)
		if partErr != nil {
			t.Fatal(partErr)
		}
		if part.FormName() == "images[]" && len(data) > 0 {
			uploadCount++
		}
	}
	if uploadCount != 3 {
		t.Fatalf("publish preparation included %d images, expected 3", uploadCount)
	}
}

func TestAIAlwaysUsesOriginalFirstSupplierImageAfterManualCoverAndReorder(t *testing.T) {
	images := normalizeCatalogImages([]CatalogImage{
		{ID: "supplier-original", Source: "supplier", URL: "https://example.test/original.jpg"},
		{ID: "supplier-second", Source: "supplier", URL: "https://example.test/second.jpg"},
	})
	if !images[0].AIInput || images[1].AIInput {
		t.Fatalf("original supplier image was not marked as AI input: %#v", images)
	}

	// Simulate a manual image becoming the cover and both supplier images being
	// reordered. The permanent marker, not gallery position, must decide.
	reordered := []CatalogImage{
		{ID: "manual-cover", Position: 0, Source: "manual", Path: "manual.jpg"},
		{ID: "supplier-second", Position: 1, Source: "supplier", URL: "https://example.test/second.jpg"},
		{ID: "supplier-original", Position: 2, Source: "supplier", URL: "https://example.test/original.jpg", AIInput: true},
	}
	selected, ok := supplierAIInputImage(reordered)
	if !ok || selected.ID != "supplier-original" {
		t.Fatalf("AI selected %q instead of the original supplier image", selected.ID)
	}
}

func TestAIDoesNotFallBackToManualImageWhenOriginalSupplierImageIsMissing(t *testing.T) {
	images := []CatalogImage{{ID: "manual-cover", Source: "manual", Path: "manual.jpg"}}
	if selected, ok := supplierAIInputImage(images); ok {
		t.Fatalf("AI unexpectedly selected manual image %#v", selected)
	}
}

func TestProcessedImagePersistenceFailureRollsBackGalleryAndStatus(t *testing.T) {
	dir := t.TempDir()
	processedPath := writeTestPNG(t, dir, "processed.png", color.RGBA{R: 80, G: 120, B: 220, A: 255})
	original := CatalogImage{ID: "supplier", Source: "supplier", AIInput: true, URL: "https://example.test/original.jpg"}
	store := &CatalogStore{
		catalogs: []UploadedCatalog{{SavedPath: filepath.Join(dir, "missing", "catalog.json"), Items: []CatalogItem{{
			ItemID: "item-1", Images: []CatalogImage{original},
		}}}},
		processedImages: map[string]string{},
		processingItems: map[string]ImageProcessingItem{},
	}

	if err := store.SetProcessedImageWithModel("item-1", processedPath, "test"); err == nil {
		t.Fatal("expected catalog persistence to fail")
	}
	images := store.catalogs[0].Items[0].Images
	if len(images) != 1 || images[0].ID != "supplier" {
		t.Fatalf("gallery was not rolled back after persistence failure: %#v", images)
	}
	if _, exists := store.processedImages["item-1"]; exists {
		t.Fatal("processed image status was committed despite persistence failure")
	}
}

func TestBulkImageAddRejectsUnknownItems(t *testing.T) {
	store := &CatalogStore{catalogs: []UploadedCatalog{}}
	_, _, err := store.AddCatalogImagesBulk([]string{"missing-item"}, []CatalogImage{{
		ID: "manual", Source: "manual", Path: "manual.jpg", Checksum: "hash",
	}}, false)
	if err == nil {
		t.Fatal("expected unknown bulk selection to fail")
	}
}

func TestGallerySaveRejectsStaleSnapshotInsteadOfDeletingNewAIImage(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	item := CatalogItem{ItemID: "item-1", Images: []CatalogImage{{
		ID: "supplier", Source: "supplier", AIInput: true, URL: "https://example.test/original.jpg",
	}}}
	writeTestCatalog(t, catalogPath, item)
	store := &CatalogStore{catalogs: []UploadedCatalog{{SavedPath: catalogPath, Items: []CatalogItem{item}}}}

	// The modal knows only the supplier image. AI completes before the user
	// presses Save, adding an image that the stale modal must not remove.
	store.catalogs[0].Items[0].Images = append([]CatalogImage{{
		ID: "new-ai", Source: "ai", Path: "processed.png",
	}}, store.catalogs[0].Items[0].Images...)
	_, err := store.ReplaceCatalogImages("item-1", []string{"supplier"}, nil, []string{"supplier"})
	if !errors.Is(err, errCatalogImagesChanged) {
		t.Fatalf("expected stale gallery conflict, got %v", err)
	}
	if len(store.catalogs[0].Items[0].Images) != 2 {
		t.Fatalf("stale save removed a newer image: %#v", store.catalogs[0].Items[0].Images)
	}
}

func TestManagedCatalogImagePathRejectsFilesOutsideStore(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "manual-images", "safe.png")
	outside := filepath.Join(filepath.Dir(root), "secret.png")
	if !managedCatalogImagePath(inside, root) {
		t.Fatal("managed image path inside the store was rejected")
	}
	if managedCatalogImagePath(outside, root) {
		t.Fatal("path outside the catalog store was accepted")
	}
	if managedCatalogImagePath(filepath.Join(root, "..", "secret.png"), root) {
		t.Fatal("traversal path outside the catalog store was accepted")
	}
}

func TestGallerySaveHTTPFlowPersistsAndReturnsPreviewURL(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	supplierPath := writeTestPNG(t, dir, "supplier.png", color.RGBA{R: 180, A: 255})
	manualPath := writeTestPNG(t, dir, "manual.png", color.RGBA{G: 180, A: 255})
	item := CatalogItem{ItemID: "item-1", NameB2B: "Test", Images: []CatalogImage{{
		ID: "supplier", Source: "supplier", AIInput: true, Path: supplierPath, Checksum: fileChecksum(supplierPath),
	}}}
	writeTestCatalog(t, catalogPath, item)
	store := &CatalogStore{
		tempDir:          dir,
		catalogs:         []UploadedCatalog{{SavedPath: catalogPath, Items: []CatalogItem{item}}},
		processedImages:  map[string]string{},
		processingItems:  map[string]ImageProcessingItem{},
		descriptionItems: map[string]DescriptionProcessingItem{},
	}
	app := &App{store: store}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("itemId", "item-1")
	_ = writer.WriteField("knownIds", `["supplier"]`)
	_ = writer.WriteField("order", `["new:0","supplier"]`)
	part, err := writer.CreateFormFile("files", "manual.png")
	if err != nil {
		t.Fatal(err)
	}
	manualBytes, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(manualBytes); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/catalog/images/save", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	app.handleCatalogImagesSave(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("gallery save returned %d: %s", response.Code, response.Body.String())
	}
	var saved CatalogItem
	if err := json.NewDecoder(response.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Images) != 2 || saved.Images[0].Source != "manual" || saved.Images[1].ID != "supplier" {
		t.Fatalf("HTTP save returned wrong gallery: %#v", saved.Images)
	}
	if !strings.HasPrefix(saved.Images[0].URL, "/api/catalog/image") {
		t.Fatalf("manual image preview URL was not decorated: %q", saved.Images[0].URL)
	}
}

func writeTestCatalog(t *testing.T, path string, item CatalogItem) {
	t.Helper()
	payload, err := json.Marshal(CatalogPayload{Items: []CatalogItem{item}})
	if err != nil || os.WriteFile(path, payload, 0o644) != nil {
		t.Fatal("failed to prepare catalog")
	}
}

func writeTestPNG(t *testing.T, dir, name string, fill color.RGBA) string {
	t.Helper()
	path := filepath.Join(dir, name)
	canvas := image.NewRGBA(image.Rect(0, 0, 96, 72))
	for y := 0; y < canvas.Bounds().Dy(); y++ {
		for x := 0; x < canvas.Bounds().Dx(); x++ {
			canvas.SetRGBA(x, y, fill)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(file, canvas)
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		t.Fatalf("failed to write test image: encode=%v close=%v", encodeErr, closeErr)
	}
	return path
}
