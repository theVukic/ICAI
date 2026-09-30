package main

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CatalogStore struct {
	mu               sync.RWMutex
	tempDir          string
	catalogs         []UploadedCatalog
	categorizations  map[string]ItemCategorySelection
	pendingPublishes map[string]CatalogItem
	processedImages  map[string]string
	processingItems  map[string]ImageProcessingItem
	descriptionItems map[string]DescriptionProcessingItem
}

var errCatalogAlreadyUploaded = errors.New("fajl je već dodat")

func NewCatalogStore() (*CatalogStore, error) {
	tempDir, err := os.MkdirTemp("", "asi-*")
	if err != nil {
		return nil, fmt.Errorf("nije moguće kreirati temp folder: %w", err)
	}

	store := &CatalogStore{
		tempDir:          tempDir,
		catalogs:         []UploadedCatalog{},
		categorizations:  map[string]ItemCategorySelection{},
		pendingPublishes: map[string]CatalogItem{},
		processedImages:  map[string]string{},
		processingItems:  map[string]ImageProcessingItem{},
		descriptionItems: map[string]DescriptionProcessingItem{},
	}

	logEvent("catalog", "catalog store initialized", map[string]any{
		"temp_dir": tempDir,
	})

	return store, nil
}

func (s *CatalogStore) TempDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tempDir
}

func (s *CatalogStore) State() CatalogState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.buildStateLocked()
}

func (s *CatalogStore) buildStateLocked() CatalogState {
	catalogs := make([]UploadedCatalog, len(s.catalogs))
	copy(catalogs, s.catalogs)

	items := make([]CatalogItem, 0)
	var lastUpdated *time.Time
	for _, catalog := range catalogs {
		for _, item := range catalog.Items {
			item.SelectionKey = catalogItemSelectionKey(item)
			if _, hidden := s.pendingPublishes[item.SelectionKey]; hidden {
				continue
			}
			if categorization, ok := s.categorizations[item.ItemID]; ok {
				copied := categorization
				item.Categorization = &copied
			}
			items = append(items, item)
		}

		if lastUpdated == nil || catalog.UploadedAt.After(*lastUpdated) {
			t := catalog.UploadedAt
			lastUpdated = &t
		}
	}

	processing := ImageProcessingState{Items: make(map[string]ImageProcessingItem, len(s.processingItems))}
	for key, value := range s.processingItems {
		processing.Items[key] = value
		switch value.Status {
		case "queued":
			processing.Queued++
		case "processing":
			processing.Working++
		case "done":
			processing.Done++
		case "error":
			processing.Failed++
		}
	}
	aiProcessing := AIProcessingState{Items: make(map[string]AIProcessingItem, len(items)), Enabled: true}
	for _, item := range items {
		imageState := s.processingItems[item.ItemID]
		descriptionState := s.descriptionItems[item.ItemID]
		aiProcessing.Items[item.ItemID] = AIProcessingItem{Image: imageState, Description: descriptionState}
		switch descriptionState.Status {
		case "queued":
			aiProcessing.Queued++
		case "processing":
			aiProcessing.Working++
		case "done":
			aiProcessing.Done++
		case "error":
			aiProcessing.Failed++
		}
	}
	return CatalogState{
		Catalogs:        catalogs,
		Items:           items,
		TempDir:         s.tempDir,
		TotalFiles:      len(catalogs),
		TotalItems:      len(items),
		LastUpdated:     lastUpdated,
		ImageProcessing: processing,
		AIProcessing:    aiProcessing,
	}
}

func (s *CatalogStore) SetImageProcessingStatus(itemID string, status ImageProcessingItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(itemID) == "" {
		return
	}
	s.processingItems[itemID] = status
	if status.Status != "done" {
		delete(s.processedImages, itemID)
	}
}

func (s *CatalogStore) ClearImageProcessingStatus(itemID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, generated := s.processedImages[itemID]; !generated {
		delete(s.processingItems, itemID)
	}
}

func (s *CatalogStore) SetProcessedImage(itemID, imagePath string) error {
	return s.SetProcessedImageWithModel(itemID, imagePath, "")
}

func (s *CatalogStore) SetProcessedImageWithModel(itemID, imagePath, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			item := &catalog.Items[itemIndex]
			if item.ItemID != itemID {
				continue
			}
			images := make([]CatalogImage, 0, len(item.Images)+1)
			images = append(images, CatalogImage{
				ID:       generateID(),
				Path:     imagePath,
				Source:   "ai",
				Checksum: fileChecksum(imagePath),
			})
			for _, image := range item.Images {
				if image.Source != "ai" {
					images = append(images, image)
				}
			}
			previousImages := item.Images
			item.Images = normalizeCatalogImages(images)
			if err := s.writeCatalogFile(*catalog); err != nil {
				item.Images = previousImages
				return fmt.Errorf("AI slika nije sačuvana u galeriju: %w", err)
			}
			s.processedImages[itemID] = imagePath
			s.processingItems[itemID] = ImageProcessingItem{Status: "done", Model: model}
			return nil
		}
	}
	return fmt.Errorf("%w: %s", errCatalogItemNotFound, itemID)
}

func (s *CatalogStore) ProcessedImage(itemID string) (string, ImageProcessingItem) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.processedImages[itemID], s.processingItems[itemID]
}

func (s *CatalogStore) SetDescriptionProcessingStatus(itemID string, status DescriptionProcessingItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(itemID) != "" {
		s.descriptionItems[itemID] = status
	}
}

func (s *CatalogStore) DescriptionProcessing(itemID string) DescriptionProcessingItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.descriptionItems[itemID]
}

func (s *CatalogStore) ResetDescriptionProcessing() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.descriptionItems = map[string]DescriptionProcessingItem{}
}

func (s *CatalogStore) SetGeneratedDescription(itemID, description, source, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			item := &catalog.Items[itemIndex]
			if item.ItemID != itemID {
				continue
			}
			item.AITechnicalDescription = strings.TrimSpace(description)
			item.UseAIContent = true
			s.descriptionItems[itemID] = DescriptionProcessingItem{Status: "done", Source: source, Model: model}
			return s.writeCatalogFile(*catalog)
		}
	}
	return fmt.Errorf("%w: %s", errCatalogItemNotFound, itemID)
}

func (s *CatalogStore) SetAIContentEnabled(itemID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			item := &catalog.Items[itemIndex]
			if item.ItemID != itemID {
				continue
			}
			item.UseAIContent = enabled
			if err := s.writeCatalogFile(*catalog); err != nil {
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("%w: %s", errCatalogItemNotFound, itemID)
}

func (s *CatalogStore) AIWorkBlocking(itemKeys, towKods []string) (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	targets := newCatalogSelectionTargets(itemKeys, towKods)
	pending, failed := 0, 0
	for _, catalog := range s.catalogs {
		for _, item := range catalog.Items {
			if !targets.Empty() && !targets.Match(item) {
				continue
			}
			for _, status := range []string{s.processingItems[item.ItemID].Status, s.descriptionItems[item.ItemID].Status} {
				switch status {
				case "queued", "processing":
					pending++
				}
			}
		}
	}
	return pending, failed
}

func catalogItemSelectionKey(item CatalogItem) string {
	return strings.TrimSpace(item.ItemID)
}

type catalogSelectionTargets struct {
	itemKeys map[string]struct{}
	towKods  map[string]struct{}
}

func newCatalogSelectionTargets(itemKeys []string, towKods []string) catalogSelectionTargets {
	targets := catalogSelectionTargets{
		itemKeys: make(map[string]struct{}, len(itemKeys)),
		towKods:  make(map[string]struct{}, len(towKods)),
	}

	for _, itemKey := range itemKeys {
		trimmed := strings.TrimSpace(itemKey)
		if trimmed == "" {
			continue
		}
		targets.itemKeys[trimmed] = struct{}{}
	}

	for _, towKod := range towKods {
		trimmed := strings.TrimSpace(towKod)
		if trimmed == "" {
			continue
		}
		targets.towKods[trimmed] = struct{}{}
	}

	return targets
}

func (t catalogSelectionTargets) Empty() bool {
	return len(t.itemKeys) == 0 && len(t.towKods) == 0
}

func (t catalogSelectionTargets) Match(item CatalogItem) bool {
	if len(t.itemKeys) > 0 {
		_, ok := t.itemKeys[catalogItemSelectionKey(item)]
		return ok
	}
	_, ok := t.towKods[strings.TrimSpace(item.TowKod)]
	return ok
}

func (s *CatalogStore) ReserveItemsForPublish(itemKeys []string, towKods []string) ([]CatalogItem, CatalogState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	targets := newCatalogSelectionTargets(itemKeys, towKods)
	if targets.Empty() {
		return nil, CatalogState{}, fmt.Errorf("odaberi barem jedan artikal")
	}

	reserved := make([]CatalogItem, 0, len(itemKeys)+len(towKods))
	for _, catalog := range s.catalogs {
		for _, item := range catalog.Items {
			item.SelectionKey = catalogItemSelectionKey(item)
			if !targets.Match(item) {
				continue
			}
			if _, alreadyPending := s.pendingPublishes[item.SelectionKey]; alreadyPending {
				continue
			}

			itemCopy := item
			if itemCopy.UseAIContent && strings.TrimSpace(itemCopy.AITechnicalDescription) != "" {
				itemCopy.TechnicalDescription = itemCopy.AITechnicalDescription
				itemCopy.TechnicalDescriptionFormat = "html"
			}
			if categorization, ok := s.categorizations[item.ItemID]; ok {
				copied := categorization
				itemCopy.Categorization = &copied
			}
			s.pendingPublishes[item.SelectionKey] = itemCopy
			reserved = append(reserved, itemCopy)
		}
	}

	if len(reserved) == 0 {
		return nil, CatalogState{}, fmt.Errorf("artikli nisu pronađeni ili su već u redu za objavu")
	}

	logEvent("publish", "items reserved for publish queue", map[string]any{
		"reserved_count": len(reserved),
		"tow_kods": func() []string {
			values := make([]string, 0, len(reserved))
			for _, item := range reserved {
				values = append(values, item.TowKod)
			}
			return values
		}(),
	})

	return reserved, s.buildStateLocked(), nil
}

func (s *CatalogStore) ReleasePublishReservations(towKods []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, key := range towKods {
		delete(s.pendingPublishes, strings.TrimSpace(key))
	}
}

func (s *CatalogStore) AddCatalog(file multipart.File, header *multipart.FileHeader) (UploadedCatalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".json" && ext != ".zip" {
		return UploadedCatalog{}, fmt.Errorf("fajl %s nije .json ili .zip", header.Filename)
	}
	for _, existingCatalog := range s.catalogs {
		if strings.EqualFold(strings.TrimSpace(existingCatalog.FileName), strings.TrimSpace(header.Filename)) {
			return UploadedCatalog{}, fmt.Errorf("%w: %s", errCatalogAlreadyUploaded, header.Filename)
		}
	}

	safeName := fmt.Sprintf("%s-%s", generateID(), filepath.Base(header.Filename))
	uploadedPath := filepath.Join(s.tempDir, safeName)
	out, err := os.Create(uploadedPath)
	if err != nil {
		return UploadedCatalog{}, fmt.Errorf("nije moguće sačuvati fajl %s: %w", header.Filename, err)
	}

	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		return UploadedCatalog{}, fmt.Errorf("nije moguće upisati fajl %s: %w", header.Filename, err)
	}
	if err := out.Close(); err != nil {
		return UploadedCatalog{}, fmt.Errorf("nije moguće zatvoriti fajl %s: %w", header.Filename, err)
	}

	var (
		payload   CatalogPayload
		savedPath string
	)
	switch ext {
	case ".json":
		payload, err = loadCatalogPayloadFromFile(uploadedPath, header.Filename)
		savedPath = uploadedPath
	case ".zip":
		payload, savedPath, err = extractCatalogZip(uploadedPath, s.tempDir, header.Filename)
		if err == nil {
			_ = os.Remove(uploadedPath)
		}
	}
	if err != nil {
		return UploadedCatalog{}, err
	}

	if len(payload.Items) == 0 {
		return UploadedCatalog{}, fmt.Errorf("fajl %s ne sadrži artikle", header.Filename)
	}

	catalogID := generateID()
	items := make([]CatalogItem, 0, len(payload.Items))
	restoredCovers := make(map[string]string)
	for _, item := range payload.Items {
		// Older ASI ZIPs kept the AI description in TechnicalDescription and the
		// supplier text in OriginalTechnicalDescription. Normalize them into the
		// new dual-version shape on import.
		if item.AITechnicalDescription != "" && item.TechnicalDescriptionFormat == "html" && strings.TrimSpace(item.OriginalTechnicalDescription) != "" {
			item.TechnicalDescription = item.OriginalTechnicalDescription
			item.TechnicalDescriptionFormat = "plain"
			item.UseAIContent = true
		} else if item.AITechnicalDescription == "" && item.TechnicalDescriptionFormat == "html" && strings.TrimSpace(item.OriginalTechnicalDescription) != "" {
			item.AITechnicalDescription = sanitizeDescriptionHTML(item.TechnicalDescription)
			item.TechnicalDescription = item.OriginalTechnicalDescription
			item.TechnicalDescriptionFormat = "plain"
			item.UseAIContent = true
		}
		if strings.TrimSpace(item.OriginalTechnicalDescription) == "" {
			item.OriginalTechnicalDescription = item.TechnicalDescription
		}
		if item.TechnicalDescriptionFormat != "html" {
			item.TechnicalDescriptionFormat = "plain"
		} else {
			item.TechnicalDescription = sanitizeDescriptionHTML(item.TechnicalDescription)
		}
		if item.AITechnicalDescription != "" {
			item.AITechnicalDescription = sanitizeDescriptionHTML(item.AITechnicalDescription)
			// Existing AI content is shown immediately after re-import, matching
			// the behaviour of freshly completed work.
			item.UseAIContent = true
		}
		processedCover := strings.TrimSpace(item.ProcessedCover)
		// The cover is only a ZIP-sidecar. Keep it out of the live catalog
		// record; its runtime association is held separately just like a newly
		// generated image.
		item.ProcessedCover = ""
		item.ItemID = generateID()
		item.SourceCatalogID = catalogID
		item.SourceFileName = header.Filename
		item.Images = normalizeCatalogImages(item.Images)
		items = append(items, item)
		if processedCover == "" {
			for _, image := range item.Images {
				if image.Source == "ai" && strings.TrimSpace(image.Path) != "" {
					restoredCovers[item.ItemID] = image.Path
					break
				}
			}
			continue
		}
		if info, statErr := os.Stat(processedCover); statErr == nil && !info.IsDir() && info.Size() > 0 {
			items[len(items)-1].UseAIContent = true
			items[len(items)-1].Images = normalizeCatalogImages(append([]CatalogImage{{
				ID: generateID(), Path: processedCover, Source: "ai", Checksum: fileChecksum(processedCover),
			}}, items[len(items)-1].Images...))
			restoredCovers[item.ItemID] = processedCover
		} else {
			logEvent("image-processing", "saved ZIP cover unavailable", map[string]any{"tow_kod": item.TowKod, "path": processedCover})
		}
	}

	catalog := UploadedCatalog{
		ID:         catalogID,
		FileName:   header.Filename,
		SavedPath:  savedPath,
		UploadedAt: time.Now(),
		TotalItems: len(items),
		Items:      items,
	}
	if err := s.writeCatalogFile(catalog); err != nil {
		return UploadedCatalog{}, fmt.Errorf("nije moguće sačuvati interne ID-jeve artikala: %w", err)
	}

	s.catalogs = append(s.catalogs, catalog)
	// An exported ASI ZIP carries the generated description in catalog.json.
	// Restore its completed state before the upload handler queues AI work, so a
	// saved catalog is displayed immediately instead of being generated again.
	for _, item := range items {
		if strings.TrimSpace(item.AITechnicalDescription) != "" {
			s.descriptionItems[item.ItemID] = DescriptionProcessingItem{Status: "done", Source: "saved"}
		}
	}
	for itemID, coverPath := range restoredCovers {
		s.processedImages[itemID] = coverPath
		s.processingItems[itemID] = ImageProcessingItem{Status: "done"}
	}
	logEvent("file", "catalog file saved", map[string]any{
		"catalog_id":  catalog.ID,
		"item_count":  catalog.TotalItems,
		"path":        savedPath,
		"source_file": header.Filename,
	})
	return catalog, nil
}

func loadCatalogPayloadFromFile(path string, sourceFileName string) (CatalogPayload, error) {
	input, err := os.Open(path)
	if err != nil {
		return CatalogPayload{}, fmt.Errorf("nije moguće otvoriti fajl %s: %w", sourceFileName, err)
	}
	defer input.Close()

	var payload CatalogPayload
	if err := json.NewDecoder(input).Decode(&payload); err != nil {
		return CatalogPayload{}, fmt.Errorf("neispravan JSON u fajlu %s: %w", sourceFileName, err)
	}

	return payload, nil
}

func extractCatalogZip(zipPath string, tempDir string, sourceFileName string) (CatalogPayload, string, error) {
	bundleDir := filepath.Join(tempDir, generateID())
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return CatalogPayload{}, "", fmt.Errorf("nije moguće pripremiti temp folder za %s: %w", sourceFileName, err)
	}

	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		_ = os.RemoveAll(bundleDir)
		return CatalogPayload{}, "", fmt.Errorf("nije moguće otvoriti ZIP %s: %w", sourceFileName, err)
	}
	defer reader.Close()

	var catalogJSONPath string
	for _, file := range reader.File {
		entryPath, pathErr := safeZipJoin(bundleDir, file.Name)
		if pathErr != nil {
			_ = os.RemoveAll(bundleDir)
			return CatalogPayload{}, "", fmt.Errorf("ZIP %s sadrži neispravnu putanju: %w", sourceFileName, pathErr)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(entryPath, 0o755); err != nil {
				_ = os.RemoveAll(bundleDir)
				return CatalogPayload{}, "", fmt.Errorf("nije moguće kreirati folder iz ZIP-a %s: %w", sourceFileName, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
			_ = os.RemoveAll(bundleDir)
			return CatalogPayload{}, "", fmt.Errorf("nije moguće pripremiti putanju iz ZIP-a %s: %w", sourceFileName, err)
		}

		input, err := file.Open()
		if err != nil {
			_ = os.RemoveAll(bundleDir)
			return CatalogPayload{}, "", fmt.Errorf("nije moguće pročitati ZIP stavku %s: %w", file.Name, err)
		}

		output, err := os.Create(entryPath)
		if err != nil {
			_ = input.Close()
			_ = os.RemoveAll(bundleDir)
			return CatalogPayload{}, "", fmt.Errorf("nije moguće sačuvati ZIP stavku %s: %w", file.Name, err)
		}

		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		_ = input.Close()
		if copyErr != nil {
			_ = os.RemoveAll(bundleDir)
			return CatalogPayload{}, "", fmt.Errorf("nije moguće raspakovati ZIP stavku %s: %w", file.Name, copyErr)
		}
		if closeErr != nil {
			_ = os.RemoveAll(bundleDir)
			return CatalogPayload{}, "", fmt.Errorf("nije moguće zatvoriti ZIP stavku %s: %w", file.Name, closeErr)
		}

		if strings.EqualFold(filepath.Base(file.Name), "catalog.json") && catalogJSONPath == "" {
			catalogJSONPath = entryPath
		}
	}

	if catalogJSONPath == "" {
		_ = os.RemoveAll(bundleDir)
		return CatalogPayload{}, "", fmt.Errorf("ZIP %s ne sadrži catalog.json", sourceFileName)
	}

	payload, err := loadCatalogPayloadFromFile(catalogJSONPath, sourceFileName)
	if err != nil {
		_ = os.RemoveAll(bundleDir)
		return CatalogPayload{}, "", err
	}

	resolveCatalogImagePaths(&payload, bundleDir)
	return payload, catalogJSONPath, nil
}

func resolveCatalogImagePaths(payload *CatalogPayload, rootDir string) {
	if payload == nil {
		return
	}

	for itemIndex := range payload.Items {
		if processedCover := strings.TrimSpace(payload.Items[itemIndex].ProcessedCover); processedCover != "" && !filepath.IsAbs(processedCover) {
			absoluteCoverPath := filepath.Join(rootDir, filepath.FromSlash(processedCover))
			if info, err := os.Stat(absoluteCoverPath); err == nil && !info.IsDir() && info.Size() > 0 {
				payload.Items[itemIndex].ProcessedCover = absoluteCoverPath
			} else {
				payload.Items[itemIndex].ProcessedCover = ""
			}
		}
		for imageIndex := range payload.Items[itemIndex].Images {
			image := &payload.Items[itemIndex].Images[imageIndex]
			trimmedPath := strings.TrimSpace(image.Path)
			if trimmedPath == "" {
				continue
			}
			if filepath.IsAbs(trimmedPath) {
				if _, err := os.Stat(trimmedPath); err == nil {
					image.Path = trimmedPath
				}
				continue
			}

			absolutePath := filepath.Join(rootDir, filepath.FromSlash(trimmedPath))
			if _, err := os.Stat(absolutePath); err == nil {
				image.Path = absolutePath
			}
		}
	}
}

func safeZipJoin(baseDir string, zipEntry string) (string, error) {
	cleanBase := filepath.Clean(baseDir)
	targetPath := filepath.Join(cleanBase, filepath.FromSlash(zipEntry))
	cleanTarget := filepath.Clean(targetPath)
	baseWithSeparator := cleanBase + string(os.PathSeparator)
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, baseWithSeparator) {
		return "", fmt.Errorf("putanja izlazi iz temp foldera")
	}
	return cleanTarget, nil
}

func (s *CatalogStore) WriteExportZIP(w io.Writer, itemKeys []string, towKods []string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := s.buildStateLocked()
	items := state.Items
	if len(items) == 0 {
		return "", fmt.Errorf("nema učitanih artikala za čuvanje")
	}

	targets := newCatalogSelectionTargets(itemKeys, towKods)
	selectedItems := make([]CatalogItem, 0, len(items))
	if targets.Empty() {
		selectedItems = append(selectedItems, items...)
	} else {
		for _, item := range items {
			item.SelectionKey = catalogItemSelectionKey(item)
			if targets.Match(item) {
				selectedItems = append(selectedItems, item)
			}
		}
	}

	if len(selectedItems) == 0 {
		return "", fmt.Errorf("artikli nisu pronađeni")
	}

	archiveName := "ASI-" + time.Now().Format("20060102-150405") + ".zip"
	zipWriter := zip.NewWriter(w)

	exportPayload := CatalogPayload{
		GeneratedAt: time.Now().Format(time.RFC3339),
		TimeZone:    time.Now().Location().String(),
		TotalItems:  len(selectedItems),
		Items:       make([]CatalogItem, 0, len(selectedItems)),
	}

	usedImagePaths := make(map[string]string)
	usedEntryNames := make(map[string]struct{})
	for _, item := range selectedItems {
		exportItem := item
		exportItem.Images = cloneCatalogImages(item.Images)

		itemSlug := sanitizeExportName(firstNonEmpty(item.TowKod, item.Index, "artikal"))
		if itemSlug == "" {
			itemSlug = "artikal"
		}

		if generatedPath, progress := s.processedImages[item.ItemID], s.processingItems[item.ItemID]; progress.Status == "done" && strings.TrimSpace(generatedPath) != "" && !containsImageSource(item.Images, "ai") {
			coverEntryName := uniqueZIPEntryName(
				fmt.Sprintf("processed/%s/cover.png", itemSlug),
				usedEntryNames,
			)
			if err := addFileToZIP(zipWriter, coverEntryName, generatedPath); err != nil {
				logEvent("catalog", "processed export cover skipped", map[string]any{
					"source_path": generatedPath,
					"reason":      err.Error(),
				})
			} else {
				exportItem.ProcessedCover = filepath.ToSlash(coverEntryName)
			}
		}
		// Keep both variants in the archive, while the primary description keeps
		// compatibility with existing ASI ZIP readers.
		if exportItem.UseAIContent && strings.TrimSpace(exportItem.AITechnicalDescription) != "" {
			exportItem.OriginalTechnicalDescription = exportItem.TechnicalDescription
			exportItem.TechnicalDescription = exportItem.AITechnicalDescription
			exportItem.TechnicalDescriptionFormat = "html"
		}

		for imageIndex := range exportItem.Images {
			image := &exportItem.Images[imageIndex]
			sourcePath := strings.TrimSpace(image.Path)
			if sourcePath == "" {
				continue
			}

			entryName, ok := usedImagePaths[sourcePath]
			if !ok {
				baseName := filepath.Base(sourcePath)
				baseName = sanitizeExportName(baseName)
				if baseName == "" {
					baseName = fmt.Sprintf("image-%d.jpg", imageIndex+1)
				}
				entryName = uniqueZIPEntryName(
					fmt.Sprintf("images/%s/%s", itemSlug, baseName),
					usedEntryNames,
				)
				if err := addFileToZIP(zipWriter, entryName, sourcePath); err == nil {
					usedImagePaths[sourcePath] = entryName
				} else {
					logEvent("catalog", "export image skipped", map[string]any{
						"source_path": sourcePath,
						"reason":      err.Error(),
					})
					continue
				}
			}

			image.Path = filepath.ToSlash(entryName)
		}

		exportPayload.Items = append(exportPayload.Items, exportItem)
	}

	catalogEntry, err := zipWriter.Create("catalog.json")
	if err != nil {
		return "", fmt.Errorf("nije moguće kreirati catalog.json u ZIP-u: %w", err)
	}
	if err := json.NewEncoder(catalogEntry).Encode(exportPayload); err != nil {
		return "", fmt.Errorf("nije moguće upisati catalog.json u ZIP: %w", err)
	}

	if err := zipWriter.Close(); err != nil {
		return "", fmt.Errorf("nije moguće završiti ZIP export: %w", err)
	}

	return archiveName, nil
}

func cloneCatalogImages(images []CatalogImage) []CatalogImage {
	if len(images) == 0 {
		return nil
	}
	cloned := make([]CatalogImage, len(images))
	copy(cloned, images)
	return cloned
}

func addFileToZIP(zipWriter *zip.Writer, entryName string, sourcePath string) error {
	input, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer input.Close()

	entryWriter, err := zipWriter.Create(filepath.ToSlash(entryName))
	if err != nil {
		return err
	}

	_, err = io.Copy(entryWriter, input)
	return err
}

func uniqueZIPEntryName(entryName string, used map[string]struct{}) string {
	normalized := filepath.ToSlash(strings.TrimSpace(entryName))
	if normalized == "" {
		normalized = "file"
	}

	if _, exists := used[normalized]; !exists {
		used[normalized] = struct{}{}
		return normalized
	}

	ext := filepath.Ext(normalized)
	base := strings.TrimSuffix(normalized, ext)
	for counter := 2; ; counter++ {
		candidate := fmt.Sprintf("%s-%d%s", base, counter, ext)
		if _, exists := used[candidate]; exists {
			continue
		}
		used[candidate] = struct{}{}
		return candidate
	}
}

func sanitizeExportName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	replacer := strings.NewReplacer(
		"\\", "-",
		"/", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
	)
	sanitized := replacer.Replace(trimmed)
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}
	return strings.Trim(sanitized, "-.")
}

func (s *CatalogStore) removeCatalogFiles(catalog UploadedCatalog) error {
	if strings.TrimSpace(catalog.SavedPath) == "" {
		return nil
	}

	targetPath := filepath.Clean(catalog.SavedPath)
	tempRoot := filepath.Clean(s.tempDir)
	tempRootWithSeparator := tempRoot + string(os.PathSeparator)

	if strings.HasPrefix(targetPath, tempRootWithSeparator) {
		parentDir := filepath.Dir(targetPath)
		if parentDir != tempRoot {
			if err := os.RemoveAll(parentDir); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
	}

	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *CatalogStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.tempDir)
	if err != nil {
		if os.IsNotExist(err) {
			if mkdirErr := os.MkdirAll(s.tempDir, 0o755); mkdirErr != nil {
				return fmt.Errorf("nije moguće ponovo kreirati temp folder: %w", mkdirErr)
			}
			s.catalogs = []UploadedCatalog{}
			s.categorizations = map[string]ItemCategorySelection{}
			s.pendingPublishes = map[string]CatalogItem{}
			s.processedImages = map[string]string{}
			s.processingItems = map[string]ImageProcessingItem{}
			s.descriptionItems = map[string]DescriptionProcessingItem{}
			return nil
		}
		return fmt.Errorf("nije moguće pročitati temp folder: %w", err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(s.tempDir, entry.Name())
		if removeErr := os.RemoveAll(entryPath); removeErr != nil {
			return fmt.Errorf("nije moguće obrisati sadržaj temp foldera: %w", removeErr)
		}
	}

	s.catalogs = []UploadedCatalog{}
	s.categorizations = map[string]ItemCategorySelection{}
	s.pendingPublishes = map[string]CatalogItem{}
	s.processedImages = map[string]string{}
	s.processingItems = map[string]ImageProcessingItem{}
	s.descriptionItems = map[string]DescriptionProcessingItem{}
	logEvent("catalog", "catalog store cleared", map[string]any{
		"temp_dir": s.tempDir,
	})
	return nil
}

func (s *CatalogStore) SaveCategorization(itemID string, selection ItemCategorySelection) (CatalogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if selection.Category == nil || selection.Category.ID == 0 {
		return CatalogItem{}, fmt.Errorf("kategorija je obavezna")
	}

	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			if catalog.Items[itemIndex].ItemID != itemID {
				continue
			}

			copied := selection
			s.categorizations[itemID] = selection
			catalog.Items[itemIndex].Categorization = &copied

			if err := s.writeCatalogFile(*catalog); err != nil {
				return CatalogItem{}, err
			}

			itemCopy := catalog.Items[itemIndex]
			itemCopy.Categorization = &copied
			logEvent("category", "single item categorization persisted", map[string]any{
				"catalog_id":  catalog.ID,
				"category_id": selection.Category.ID,
				"item_id":     itemID,
			})
			return itemCopy, nil
		}
	}

	return CatalogItem{}, fmt.Errorf("%w: %s", errCatalogItemNotFound, itemID)
}

func (s *CatalogStore) SaveCategorizationBulk(itemKeys []string, towKods []string, selection ItemCategorySelection) ([]CatalogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if selection.Category == nil || selection.Category.ID == 0 {
		return nil, fmt.Errorf("kategorija je obavezna")
	}

	targets := newCatalogSelectionTargets(itemKeys, towKods)
	if targets.Empty() {
		return nil, fmt.Errorf("odaberi barem jedan artikal")
	}

	updatedByCatalog := make(map[int]bool)
	updatedItems := make([]CatalogItem, 0, len(itemKeys)+len(towKods))

	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			item := &catalog.Items[itemIndex]
			item.SelectionKey = catalogItemSelectionKey(*item)
			if !targets.Match(*item) {
				continue
			}

			copied := selection
			s.categorizations[item.ItemID] = selection
			item.Categorization = &copied

			itemCopy := *item
			itemCopy.Categorization = &copied
			updatedItems = append(updatedItems, itemCopy)
			updatedByCatalog[catalogIndex] = true
		}
	}

	if len(updatedItems) == 0 {
		return nil, fmt.Errorf("artikli nisu pronađeni")
	}

	for catalogIndex := range updatedByCatalog {
		if err := s.writeCatalogFile(s.catalogs[catalogIndex]); err != nil {
			return nil, err
		}
	}

	logEvent("category", "bulk categorization persisted", map[string]any{
		"category_id": selection.Category.ID,
		"item_count":  len(updatedItems),
	})
	return updatedItems, nil
}

func (s *CatalogStore) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	logEvent("catalog", "catalog store cleanup", map[string]any{
		"temp_dir": s.tempDir,
	})
	return os.RemoveAll(s.tempDir)
}

func generateID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
