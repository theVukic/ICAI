package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CatalogStore struct {
	mu              sync.RWMutex
	tempDir         string
	catalogs        []UploadedCatalog
	categorizations map[string]ItemCategorySelection
}

func NewCatalogStore() (*CatalogStore, error) {
	tempDir, err := os.MkdirTemp("", "icai-*")
	if err != nil {
		return nil, fmt.Errorf("nije moguće kreirati temp folder: %w", err)
	}

	return &CatalogStore{
		tempDir:         tempDir,
		catalogs:        []UploadedCatalog{},
		categorizations: map[string]ItemCategorySelection{},
	}, nil
}

func (s *CatalogStore) TempDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tempDir
}

func (s *CatalogStore) State() CatalogState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	catalogs := make([]UploadedCatalog, len(s.catalogs))
	copy(catalogs, s.catalogs)

	items := make([]CatalogItem, 0)
	seen := make(map[string]struct{})

	var lastUpdated *time.Time
	for _, catalog := range catalogs {
		for _, item := range catalog.Items {
			key := item.TowKod + "|" + item.Index
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			if categorization, ok := s.categorizations[item.TowKod]; ok {
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

func (s *CatalogStore) AddCatalog(file multipart.File, header *multipart.FileHeader) (UploadedCatalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if filepath.Ext(header.Filename) != ".json" {
		return UploadedCatalog{}, fmt.Errorf("fajl %s nije .json", header.Filename)
	}

	payload := CatalogPayload{}
	if err := json.NewDecoder(file).Decode(&payload); err != nil {
		return UploadedCatalog{}, fmt.Errorf("neispravan JSON u fajlu %s: %w", header.Filename, err)
	}

	if len(payload.Items) == 0 {
		return UploadedCatalog{}, fmt.Errorf("fajl %s ne sadrži artikle", header.Filename)
	}

	if _, err := file.Seek(0, 0); err != nil {
		return UploadedCatalog{}, fmt.Errorf("nije moguće vratiti čitanje fajla %s: %w", header.Filename, err)
	}

	safeName := fmt.Sprintf("%s-%s", generateID(), filepath.Base(header.Filename))
	savedPath := filepath.Join(s.tempDir, safeName)
	out, err := os.Create(savedPath)
	if err != nil {
		return UploadedCatalog{}, fmt.Errorf("nije moguće sačuvati fajl %s: %w", header.Filename, err)
	}
	defer out.Close()

	if _, err := out.ReadFrom(file); err != nil {
		return UploadedCatalog{}, fmt.Errorf("nije moguće upisati fajl %s: %w", header.Filename, err)
	}

	catalogID := generateID()
	items := make([]CatalogItem, 0, len(payload.Items))
	for _, item := range payload.Items {
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

	s.catalogs = append(s.catalogs, catalog)
	return catalog, nil
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
	return nil
}

func (s *CatalogStore) SaveCategorization(towKod string, selection ItemCategorySelection) (CatalogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if selection.Category == nil || selection.Category.ID == 0 {
		return CatalogItem{}, fmt.Errorf("kategorija je obavezna")
	}

	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			if catalog.Items[itemIndex].TowKod != towKod {
				continue
			}

			copied := selection
			s.categorizations[towKod] = selection
			catalog.Items[itemIndex].Categorization = &copied

			if err := s.writeCatalogFile(*catalog); err != nil {
				return CatalogItem{}, err
			}

			itemCopy := catalog.Items[itemIndex]
			itemCopy.Categorization = &copied
			return itemCopy, nil
		}
	}

	return CatalogItem{}, fmt.Errorf("%w: %s", errCatalogItemNotFound, towKod)
}

func (s *CatalogStore) SaveCategorizationBulk(towKods []string, selection ItemCategorySelection) ([]CatalogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if selection.Category == nil || selection.Category.ID == 0 {
		return nil, fmt.Errorf("kategorija je obavezna")
	}

	targets := make(map[string]struct{}, len(towKods))
	for _, towKod := range towKods {
		trimmed := strings.TrimSpace(towKod)
		if trimmed == "" {
			continue
		}
		targets[trimmed] = struct{}{}
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("odaberi barem jedan artikal")
	}

	updatedByCatalog := make(map[int]bool)
	updatedItems := make([]CatalogItem, 0, len(targets))

	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			item := &catalog.Items[itemIndex]
			if _, ok := targets[item.TowKod]; !ok {
				continue
			}

			copied := selection
			s.categorizations[item.TowKod] = selection
			item.Categorization = &copied

			itemCopy := *item
			itemCopy.Categorization = &copied
			updatedItems = append(updatedItems, itemCopy)
			updatedByCatalog[catalogIndex] = true
			delete(targets, item.TowKod)
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

	return updatedItems, nil
}

func (s *CatalogStore) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(s.tempDir)
}

func generateID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
