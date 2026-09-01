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

	return CatalogState{
		Catalogs:    catalogs,
		Items:       items,
		TempDir:     s.tempDir,
		TotalFiles:  len(catalogs),
		TotalItems:  len(items),
		LastUpdated: lastUpdated,
	}
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
	for _, item := range payload.Items {
		item.ItemID = generateID()
		item.SourceCatalogID = catalogID
		item.SourceFileName = header.Filename
		items = append(items, item)
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
