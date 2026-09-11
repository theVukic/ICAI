package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

var errCatalogItemNotFound = errors.New("artikal nije pronađen")

func (a *App) handleCatalogItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := itemIDFromPath(strings.TrimPrefix(r.URL.Path, "/api/catalog/item"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, getErr := a.store.GetItemByID(itemID)
		if getErr != nil {
			if errors.Is(getErr, errCatalogItemNotFound) {
				writeError(w, http.StatusNotFound, getErr.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, getErr.Error())
			return
		}
		a.decorateCatalogItem(&item)
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var req CatalogItemEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "neispravan JSON body")
			return
		}

		item, updateErr := a.store.UpdateItemByID(itemID, req)
		if updateErr != nil {
			if errors.Is(updateErr, errCatalogItemNotFound) {
				writeError(w, http.StatusNotFound, updateErr.Error())
				return
			}
			writeError(w, http.StatusBadRequest, updateErr.Error())
			return
		}
		a.decorateCatalogItem(&item)

		logEvent("catalog", "single item updated", map[string]any{
			"price":   item.RetailPrice,
			"item_id": itemID,
		})
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleCatalogBulkEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CatalogBulkEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "neispravan JSON body")
		return
	}

	items, err := a.store.UpdateItemsBulk(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	logEvent("catalog", "bulk edit completed", map[string]any{
		"item_count": len(items),
		"tow_kods":   req.TowKods,
	})
	writeJSON(w, http.StatusOK, items)
}

func (a *App) handleCatalogBulkDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CatalogBulkDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "neispravan JSON body")
		return
	}

	if err := a.store.DeleteItemsBulk(req.ItemKeys, req.TowKods); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	logEvent("catalog", "bulk delete completed", map[string]any{
		"tow_kods": req.TowKods,
	})
	writeJSON(w, http.StatusOK, a.store.State())
}

func itemIDFromPath(path string) (string, error) {
	const prefix = "/"

	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("neispravna putanja")
	}

	rawCode := strings.TrimPrefix(path, prefix)
	rawCode = strings.Trim(rawCode, "/")
	if rawCode == "" {
		return "", fmt.Errorf("ID artikla je obavezan")
	}

	code, err := url.PathUnescape(rawCode)
	if err != nil {
		return "", fmt.Errorf("ID artikla nije validan")
	}

	return code, nil
}

func (s *CatalogStore) GetItemByID(itemID string) (CatalogItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, catalog := range s.catalogs {
		for _, item := range catalog.Items {
			if item.ItemID == itemID {
				if categorization, ok := s.categorizations[itemID]; ok {
					copied := categorization
					item.Categorization = &copied
				}
				return item, nil
			}
		}
	}

	return CatalogItem{}, fmt.Errorf("%w: %s", errCatalogItemNotFound, itemID)
}

func (s *CatalogStore) UpdateItemByID(itemID string, req CatalogItemEditRequest) (CatalogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := strings.TrimSpace(req.Name)
	description := strings.TrimSpace(req.Description)
	if name == "" {
		return CatalogItem{}, fmt.Errorf("name je obavezan")
	}
	if req.Price < 0 {
		return CatalogItem{}, fmt.Errorf("cijena ne može biti negativna")
	}

	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			if catalog.Items[itemIndex].ItemID != itemID {
				continue
			}

			item := &catalog.Items[itemIndex]
			item.NameB2B = name
			item.RetailPrice = req.Price
			// The edit form includes its current description with every save. Do
			// not mistake that unchanged value for a manual description edit.
			// When the card's AI switch is active, this field edits the AI variant;
			// otherwise it edits the supplier/original variant.
			if descriptionChanged(*item, description) {
				if item.UseAIContent && strings.TrimSpace(item.AITechnicalDescription) != "" {
					item.AITechnicalDescription = sanitizeDescriptionHTML(description)
					item.UseAIContent = true
					s.descriptionItems[itemID] = DescriptionProcessingItem{Status: "done", Source: "manual"}
				} else {
					item.TechnicalDescription = description
					item.OriginalTechnicalDescription = description
					item.TechnicalDescriptionFormat = "plain"
					item.UseAIContent = false
				}
			}

			if err := s.writeCatalogFile(*catalog); err != nil {
				return CatalogItem{}, err
			}

			return catalog.Items[itemIndex], nil
		}
	}

	return CatalogItem{}, fmt.Errorf("%w: %s", errCatalogItemNotFound, itemID)
}

func descriptionChanged(item CatalogItem, submitted string) bool {
	if item.UseAIContent && strings.TrimSpace(item.AITechnicalDescription) != "" {
		return sanitizeDescriptionHTML(item.AITechnicalDescription) != sanitizeDescriptionHTML(submitted)
	}
	return strings.TrimSpace(item.TechnicalDescription) != strings.TrimSpace(submitted)
}

func (s *CatalogStore) writeCatalogFile(catalog UploadedCatalog) error {
	payloadBytes, err := os.ReadFile(catalog.SavedPath)
	if err != nil {
		return fmt.Errorf("nije moguće pročitati temp fajl: %w", err)
	}

	var payload CatalogPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return fmt.Errorf("nije moguće parsirati temp fajl: %w", err)
	}

	// Catalog rows are matched only by their application-generated ItemID.
	// Rebuilding this list by TowKod or Index would merge distinct products.
	payload.Items = catalog.Items
	payload.TotalItems = len(catalog.Items)

	output, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("nije moguće pripremiti temp fajl: %w", err)
	}

	output = append(output, '\n')
	if err := os.WriteFile(catalog.SavedPath, output, 0o644); err != nil {
		return fmt.Errorf("nije moguće upisati temp fajl: %w", err)
	}

	logEvent("file", "catalog temp file updated", map[string]any{
		"catalog_id":  catalog.ID,
		"path":        catalog.SavedPath,
		"size_bytes":  len(output),
		"source_file": catalog.FileName,
	})
	return nil
}

func (s *CatalogStore) UpdateItemsBulk(req CatalogBulkEditRequest) ([]CatalogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(req.ItemKeys) == 0 && len(req.TowKods) == 0 {
		return nil, fmt.Errorf("odaberi barem jedan artikal")
	}

	if req.Name == nil && req.Price == nil && req.Description == nil && req.CurrencyCode == nil {
		return nil, fmt.Errorf("popuni barem jedno polje za izmjenu")
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		req.Name = &trimmed
	}

	if req.Description != nil {
		trimmed := strings.TrimSpace(*req.Description)
		req.Description = &trimmed
	}

	if req.CurrencyCode != nil {
		trimmed := strings.TrimSpace(*req.CurrencyCode)
		req.CurrencyCode = &trimmed
	}

	if req.Price != nil && *req.Price < 0 {
		return nil, fmt.Errorf("cijena ne može biti negativna")
	}

	targets := newCatalogSelectionTargets(req.ItemKeys, req.TowKods)
	if targets.Empty() {
		return nil, fmt.Errorf("odaberi barem jedan artikal")
	}

	updatedByCatalog := make(map[int]bool)
	updatedItems := make([]CatalogItem, 0, len(req.ItemKeys)+len(req.TowKods))

	for catalogIndex := range s.catalogs {
		catalog := &s.catalogs[catalogIndex]
		for itemIndex := range catalog.Items {
			item := &catalog.Items[itemIndex]
			item.SelectionKey = catalogItemSelectionKey(*item)
			if !targets.Match(*item) {
				continue
			}

			if req.Name != nil {
				item.NameB2B = *req.Name
			}
			if req.Price != nil {
				item.RetailPrice = *req.Price
			}
			if req.Description != nil {
				item.TechnicalDescription = *req.Description
				item.OriginalTechnicalDescription = *req.Description
				item.TechnicalDescriptionFormat = "plain"
				item.UseAIContent = false
			}
			if req.CurrencyCode != nil && *req.CurrencyCode != "" {
				item.CurrencyCode = *req.CurrencyCode
			}

			updatedItems = append(updatedItems, *item)
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

	return updatedItems, nil
}

func (s *CatalogStore) DeleteItemsBulk(itemKeys []string, towKods []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targets := newCatalogSelectionTargets(itemKeys, towKods)
	if targets.Empty() {
		return fmt.Errorf("odaberi barem jedan artikal")
	}

	nextCatalogs := make([]UploadedCatalog, 0, len(s.catalogs))
	deletedCount := 0

	for _, catalog := range s.catalogs {
		filteredItems := make([]CatalogItem, 0, len(catalog.Items))
		removedFromCatalog := false

		for _, item := range catalog.Items {
			item.SelectionKey = catalogItemSelectionKey(item)
			if targets.Match(item) {
				deletedCount++
				removedFromCatalog = true
				delete(s.categorizations, item.ItemID)
				delete(s.pendingPublishes, item.SelectionKey)
				if processedPath := s.processedImages[item.ItemID]; processedPath != "" {
					_ = os.Remove(processedPath)
				}
				delete(s.processedImages, item.ItemID)
				delete(s.processingItems, item.ItemID)
				delete(s.descriptionItems, item.ItemID)
				continue
			}

			filteredItems = append(filteredItems, item)
		}

		if !removedFromCatalog {
			nextCatalogs = append(nextCatalogs, catalog)
			continue
		}

		if len(filteredItems) == 0 {
			if err := s.removeCatalogFiles(catalog); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("nije moguće obrisati temp fajl: %w", err)
			}
			logEvent("file", "catalog temp file removed", map[string]any{
				"catalog_id":  catalog.ID,
				"path":        catalog.SavedPath,
				"source_file": catalog.FileName,
			})
			continue
		}

		catalog.Items = filteredItems
		catalog.TotalItems = len(filteredItems)
		if err := s.writeCatalogFile(catalog); err != nil {
			return err
		}
		nextCatalogs = append(nextCatalogs, catalog)
	}

	if deletedCount == 0 {
		return fmt.Errorf("artikli nisu pronađeni")
	}

	s.catalogs = nextCatalogs
	logEvent("catalog", "items deleted from store", map[string]any{
		"deleted_count": deletedCount,
	})
	return nil
}
