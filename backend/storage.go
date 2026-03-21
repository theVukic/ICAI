package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CatalogStore struct {
	mu       sync.RWMutex
	tempDir  string
	catalogs []UploadedCatalog
}

func NewCatalogStore() (*CatalogStore, error) {
	tempDir, err := os.MkdirTemp("", "icaiapp-catalogs-*")
	if err != nil {
		return nil, fmt.Errorf("nije moguće kreirati temp folder: %w", err)
	}

	return &CatalogStore{
		tempDir:  tempDir,
		catalogs: []UploadedCatalog{},
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

	if err := os.RemoveAll(s.tempDir); err != nil {
		return fmt.Errorf("nije moguće obrisati temp folder: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "icaiapp-catalogs-*")
	if err != nil {
		return fmt.Errorf("nije moguće ponovo kreirati temp folder: %w", err)
	}

	s.tempDir = tempDir
	s.catalogs = []UploadedCatalog{}
	return nil
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
