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
	towKod, err := itemCodeFromPath(strings.TrimPrefix(r.URL.Path, "/api/catalog/item"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, getErr := a.store.GetItemByTowKod(towKod)
		if getErr != nil {
			if errors.Is(getErr, errCatalogItemNotFound) {
				writeError(w, http.StatusNotFound, getErr.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, getErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var req CatalogItemEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "neispravan JSON body")
			return
		}

		item, updateErr := a.store.UpdateItemByTowKod(towKod, req)
		if updateErr != nil {
			if errors.Is(updateErr, errCatalogItemNotFound) {
				writeError(w, http.StatusNotFound, updateErr.Error())
				return
			}
			writeError(w, http.StatusBadRequest, updateErr.Error())
			return
		}

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

	writeJSON(w, http.StatusOK, items)
}

func itemCodeFromPath(path string) (string, error) {
	const prefix = "/"

	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("neispravna putanja")
	}

	rawCode := strings.TrimPrefix(path, prefix)
	rawCode = strings.Trim(rawCode, "/")
	if rawCode == "" {
		return "", fmt.Errorf("towKod je obavezan")
	}

	code, err := url.PathUnescape(rawCode)
	if err != nil {
		return "", fmt.Errorf("towKod nije validan")
	}

	return code, nil
}

func (s *CatalogStore) GetItemByTowKod(towKod string) (CatalogItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, catalog := range s.catalogs {
		for _, item := range catalog.Items {
			if item.TowKod == towKod {
				if categorization, ok := s.categorizations[towKod]; ok {
					copied := categorization
					item.Categorization = &copied
				}
				return item, nil
			}
		}
	}

	return CatalogItem{}, fmt.Errorf("%w: %s", errCatalogItemNotFound, towKod)
}

func (s *CatalogStore) UpdateItemByTowKod(towKod string, req CatalogItemEditRequest) (CatalogItem, error) {
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
			if catalog.Items[itemIndex].TowKod != towKod {
				continue
			}

			catalog.Items[itemIndex].NameB2B = name
			catalog.Items[itemIndex].RetailPrice = req.Price
			catalog.Items[itemIndex].TechnicalDescription = description

			if err := s.writeCatalogFile(*catalog); err != nil {
				return CatalogItem{}, err
			}

			return catalog.Items[itemIndex], nil
		}
	}

	return CatalogItem{}, fmt.Errorf("%w: %s", errCatalogItemNotFound, towKod)
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

	updated := false
	for itemIndex := range payload.Items {
		for _, currentItem := range catalog.Items {
			if payload.Items[itemIndex].TowKod != currentItem.TowKod {
				continue
			}

			payload.Items[itemIndex].NameB2B = currentItem.NameB2B
			payload.Items[itemIndex].RetailPrice = currentItem.RetailPrice
			payload.Items[itemIndex].TechnicalDescription = currentItem.TechnicalDescription
			payload.Items[itemIndex].Categorization = currentItem.Categorization
			updated = true
			break
		}
	}

	if !updated {
		return fmt.Errorf("artikal nije pronađen u temp fajlu")
	}

	output, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("nije moguće pripremiti temp fajl: %w", err)
	}

	output = append(output, '\n')
	if err := os.WriteFile(catalog.SavedPath, output, 0o644); err != nil {
		return fmt.Errorf("nije moguće upisati temp fajl: %w", err)
	}

	return nil
}

func (s *CatalogStore) UpdateItemsBulk(req CatalogBulkEditRequest) ([]CatalogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(req.TowKods) == 0 {
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

	targets := make(map[string]struct{}, len(req.TowKods))
	for _, towKod := range req.TowKods {
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

			if req.Name != nil {
				item.NameB2B = *req.Name
			}
			if req.Price != nil {
				item.RetailPrice = *req.Price
			}
			if req.Description != nil {
				item.TechnicalDescription = *req.Description
			}
			if req.CurrencyCode != nil && *req.CurrencyCode != "" {
				item.CurrencyCode = *req.CurrencyCode
			}

			updatedItems = append(updatedItems, *item)
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
