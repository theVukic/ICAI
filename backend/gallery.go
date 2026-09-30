package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxManualImageBytes = 32 << 20
const maxGalleryRequestBytes = 256 << 20

var errCatalogImagesChanged = errors.New("galerija je promijenjena u međuvremenu; otvori je ponovo")

func (a *App) handleCatalogImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	image, err := a.store.GetCatalogImage(r.URL.Query().Get("itemId"), r.URL.Query().Get("imageId"))
	if err != nil || strings.TrimSpace(image.Path) == "" || !managedCatalogImagePath(image.Path, a.store.TempDir()) {
		writeError(w, http.StatusNotFound, "slika nije pronađena")
		return
	}
	info, err := os.Stat(image.Path)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "slika nije dostupna")
		return
	}
	http.ServeFile(w, r, image.Path)
}

func managedCatalogImagePath(path, root string) bool {
	cleanPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	cleanRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || relative == "." {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func (a *App) handleCatalogImagesSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxGalleryRequestBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "upload slika nije validan")
		return
	}
	itemID := strings.TrimSpace(r.FormValue("itemId"))
	var order []string
	if err := json.Unmarshal([]byte(r.FormValue("order")), &order); err != nil {
		writeError(w, http.StatusBadRequest, "redoslijed slika nije validan")
		return
	}
	var knownIDs []string
	if err := json.Unmarshal([]byte(r.FormValue("knownIds")), &knownIDs); err != nil {
		writeError(w, http.StatusBadRequest, "stanje galerije nije validno")
		return
	}
	images, err := a.saveManualImages(r.MultipartForm.File["files"])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := a.store.ReplaceCatalogImages(itemID, order, images, knownIDs)
	if err != nil {
		if errors.Is(err, errCatalogImagesChanged) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.decorateCatalogItem(&item)
	writeJSON(w, http.StatusOK, item)
}

func (a *App) handleCatalogImagesBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxGalleryRequestBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "upload slika nije validan")
		return
	}
	var itemKeys []string
	if err := json.Unmarshal([]byte(r.FormValue("itemKeys")), &itemKeys); err != nil || len(itemKeys) == 0 {
		writeError(w, http.StatusBadRequest, "odaberi barem jedan artikal")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.FormValue("mode")))
	if mode != "append" && mode != "cover" {
		writeError(w, http.StatusBadRequest, "nepoznat način dodavanja slika")
		return
	}
	images, err := a.saveManualImages(r.MultipartForm.File["files"])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(images) == 0 {
		writeError(w, http.StatusBadRequest, "izaberi barem jednu sliku")
		return
	}
	if mode == "cover" && len(images) != 1 {
		writeError(w, http.StatusBadRequest, "za naslovnu sliku izaberi jedan fajl")
		return
	}
	updated, skipped, err := a.store.AddCatalogImagesBulk(itemKeys, images, mode == "cover")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state := a.store.State()
	a.decorateAIProcessingState(&state)
	writeJSON(w, http.StatusOK, map[string]any{"state": state, "updated": updated, "skipped": skipped})
}

func (a *App) saveManualImages(headers []*multipart.FileHeader) ([]CatalogImage, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	dir := filepath.Join(a.store.TempDir(), "manual-images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("nije moguće pripremiti folder za slike")
	}
	images := make([]CatalogImage, 0, len(headers))
	for _, header := range headers {
		if header.Size > maxManualImageBytes {
			return nil, fmt.Errorf("slika %s je prevelika", header.Filename)
		}
		file, err := header.Open()
		if err != nil {
			return nil, fmt.Errorf("nije moguće otvoriti sliku %s", header.Filename)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxManualImageBytes+1))
		_ = file.Close()
		if readErr != nil || len(data) == 0 || len(data) > maxManualImageBytes {
			return nil, fmt.Errorf("nije moguće pročitati sliku %s", header.Filename)
		}
		contentType := http.DetectContentType(data)
		ext := ""
		switch contentType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/webp":
			ext = ".webp"
		default:
			return nil, fmt.Errorf("%s nije JPG, PNG ili WebP slika", header.Filename)
		}
		sum := sha256.Sum256(data)
		checksum := hex.EncodeToString(sum[:])
		path := filepath.Join(dir, checksum+ext)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return nil, fmt.Errorf("nije moguće sačuvati sliku %s", header.Filename)
			}
		}
		images = append(images, CatalogImage{ID: generateID(), Path: path, Source: "manual", Checksum: checksum})
	}
	return images, nil
}

func (s *CatalogStore) GetCatalogImage(itemID, imageID string) (CatalogImage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, catalog := range s.catalogs {
		for _, item := range catalog.Items {
			if item.ItemID != itemID {
				continue
			}
			for _, image := range item.Images {
				if image.ID == imageID {
					return image, nil
				}
			}
		}
	}
	return CatalogImage{}, errCatalogItemNotFound
}

func (s *CatalogStore) ReplaceCatalogImages(itemID string, order []string, uploaded []CatalogImage, knownIDs []string) (CatalogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			item := &catalog.Items[itemIndex]
			if item.ItemID != itemID {
				continue
			}
			currentIDs := make([]string, 0, len(item.Images))
			for _, image := range item.Images {
				currentIDs = append(currentIDs, image.ID)
			}
			if !sameStringSet(currentIDs, knownIDs) {
				return CatalogItem{}, errCatalogImagesChanged
			}
			existing := make(map[string]CatalogImage, len(item.Images))
			existingByChecksum := make(map[string]CatalogImage, len(item.Images))
			for _, image := range item.Images {
				existing[image.ID] = image
				if image.Checksum != "" {
					existingByChecksum[image.Checksum] = image
				}
			}
			next := make([]CatalogImage, 0, len(order))
			usedIDs := make(map[string]bool)
			usedChecksums := make(map[string]bool)
			for _, token := range order {
				var image CatalogImage
				if strings.HasPrefix(token, "new:") {
					var uploadIndex int
					if _, err := fmt.Sscanf(token, "new:%d", &uploadIndex); err != nil || uploadIndex < 0 || uploadIndex >= len(uploaded) {
						return CatalogItem{}, fmt.Errorf("redoslijed sadrži nepoznatu sliku")
					}
					image = uploaded[uploadIndex]
					if original, ok := existingByChecksum[image.Checksum]; ok {
						image = original
					}
				} else {
					var ok bool
					image, ok = existing[token]
					if !ok {
						return CatalogItem{}, fmt.Errorf("redoslijed sadrži nepoznatu sliku")
					}
				}
				if usedIDs[image.ID] || (image.Checksum != "" && usedChecksums[image.Checksum]) {
					continue
				}
				usedIDs[image.ID] = true
				if image.Checksum != "" {
					usedChecksums[image.Checksum] = true
				}
				image.Position = len(next)
				next = append(next, image)
			}
			previous := item.Images
			item.Images = next
			if err := s.writeCatalogFile(*catalog); err != nil {
				item.Images = previous
				return CatalogItem{}, err
			}
			if !containsImageSource(next, "ai") {
				delete(s.processedImages, itemID)
				delete(s.processingItems, itemID)
			}
			return *item, nil
		}
	}
	return CatalogItem{}, fmt.Errorf("%w: %s", errCatalogItemNotFound, itemID)
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func (s *CatalogStore) AddCatalogImagesBulk(itemKeys []string, uploaded []CatalogImage, cover bool) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	targets := newCatalogSelectionTargets(itemKeys, nil)
	updated, skipped := 0, 0
	changedCatalogs := make(map[int]bool)
	for catalogIndex := range s.catalogs {
		for itemIndex := range s.catalogs[catalogIndex].Items {
			item := &s.catalogs[catalogIndex].Items[itemIndex]
			item.SelectionKey = catalogItemSelectionKey(*item)
			if !targets.Match(*item) {
				continue
			}
			added := make([]CatalogImage, 0, len(uploaded))
			itemChanged := false
			for _, source := range uploaded {
				duplicateIndex := imageIndexByChecksum(item.Images, source.Checksum)
				if duplicateIndex >= 0 {
					skipped++
					if cover {
						duplicate := item.Images[duplicateIndex]
						item.Images = append([]CatalogImage{duplicate}, append(item.Images[:duplicateIndex], item.Images[duplicateIndex+1:]...)...)
						changedCatalogs[catalogIndex] = true
						itemChanged = true
					}
					continue
				}
				copyImage := source
				copyImage.ID = generateID()
				added = append(added, copyImage)
			}
			if len(added) > 0 {
				if cover {
					item.Images = append(added, item.Images...)
				} else {
					item.Images = append(item.Images, added...)
				}
				itemChanged = true
				changedCatalogs[catalogIndex] = true
			}
			if itemChanged {
				updated++
			}
			renumberCatalogImages(item.Images)
		}
	}
	if len(changedCatalogs) == 0 {
		if skipped > 0 {
			return 0, skipped, nil
		}
		return 0, 0, fmt.Errorf("artikli nisu pronađeni")
	}
	indexes := make([]int, 0, len(changedCatalogs))
	for index := range changedCatalogs {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		if err := s.writeCatalogFile(s.catalogs[index]); err != nil {
			return 0, skipped, err
		}
	}
	return updated, skipped, nil
}

func normalizeCatalogImages(images []CatalogImage) []CatalogImage {
	hasAIInput := false
	for index := range images {
		if strings.TrimSpace(images[index].ID) == "" {
			images[index].ID = generateID()
		}
		if strings.TrimSpace(images[index].Source) == "" {
			images[index].Source = "supplier"
		}
		images[index].Position = index
		if images[index].Checksum == "" && images[index].Path != "" {
			images[index].Checksum = fileChecksum(images[index].Path)
		}
		if images[index].Source == "supplier" && images[index].AIInput {
			hasAIInput = true
		}
	}
	// Mark this once, while the supplier catalog is being imported. The marker
	// survives gallery reordering, a new manual cover and ZIP export/import.
	if !hasAIInput {
		for index := range images {
			if images[index].Source == "supplier" {
				images[index].AIInput = true
				break
			}
		}
	}
	return images
}

func renumberCatalogImages(images []CatalogImage) {
	for index := range images {
		images[index].Position = index
	}
}

func containsImageSource(images []CatalogImage, source string) bool {
	for _, image := range images {
		if image.Source == source {
			return true
		}
	}
	return false
}

func imageIndexByChecksum(images []CatalogImage, checksum string) int {
	if checksum == "" {
		return -1
	}
	for index, image := range images {
		if image.Checksum == checksum {
			return index
		}
	}
	return -1
}

func fileChecksum(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}
