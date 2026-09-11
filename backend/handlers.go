package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "BACKEND RADI.",
	})
}

func (a *App) handleCatalogState(w http.ResponseWriter, r *http.Request) {
	logEvent("catalog", "state requested", map[string]any{
		"items": a.store.State().TotalItems,
		"files": a.store.State().TotalFiles,
	})
	state := a.store.State()
	a.decorateAIProcessingState(&state)
	writeJSON(w, http.StatusOK, state)
}

// handleProcessedCatalogImage serves only a generated image already associated
// with a catalog item; a filesystem path is never accepted from the client.
func (a *App) handleProcessedCatalogImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	itemID := r.URL.Query().Get("itemId")
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "nedostaje identifikator artikla")
		return
	}
	if _, err := a.store.GetItemByID(itemID); err != nil {
		writeError(w, http.StatusNotFound, "artikal nije pronađen")
		return
	}
	imagePath, progress := a.store.ProcessedImage(itemID)
	if progress.Status != "done" || imagePath == "" {
		writeError(w, http.StatusNotFound, "obrađena slika nije dostupna")
		return
	}
	// A generated image is immutable for the version in its URL. Keeping it in
	// the private browser cache prevents a visible reload when using arrows.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, imagePath)
}

func (a *App) handleCatalogUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "upload forma nije validna")
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "nije poslat nijedan .json ili .zip fajl")
		return
	}

	logEvent("catalog", "upload started", map[string]any{
		"file_count": len(files),
		"files": func() []string {
			names := make([]string, 0, len(files))
			for _, header := range files {
				names = append(names, header.Filename)
			}
			return names
		}(),
	})

	skippedDuplicates := make([]string, 0)
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "nije moguće otvoriti poslati fajl")
			return
		}

		_, saveErr := a.store.AddCatalog(file, header)
		_ = file.Close()
		if saveErr != nil {
			if errors.Is(saveErr, errCatalogAlreadyUploaded) {
				skippedDuplicates = append(skippedDuplicates, header.Filename)
				continue
			}
			writeError(w, http.StatusBadRequest, saveErr.Error())
			return
		}
	}

	state := a.store.State()
	if len(skippedDuplicates) > 0 {
		logEvent("catalog", "duplicate upload skipped", map[string]any{
			"duplicate_count": len(skippedDuplicates),
			"files":           skippedDuplicates,
		})
	}
	logEvent("catalog", "upload completed", map[string]any{
		"files_total":        state.TotalFiles,
		"items_total":        state.TotalItems,
		"skipped_duplicates": len(skippedDuplicates),
	})
	state = a.store.State()
	a.decorateAIProcessingState(&state)
	writeJSON(w, http.StatusCreated, state)
}

func (a *App) handleAIProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req AIProcessRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "neispravan AI zahtjev")
		return
	}
	if req.Kind != "image" && req.Kind != "description" {
		writeError(w, http.StatusBadRequest, "nepoznata AI obrada")
		return
	}
	if err := a.enqueueAISelection(req.ItemKeys, req.TowKods, req.Kind); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state := a.store.State()
	a.decorateAIProcessingState(&state)
	writeJSON(w, http.StatusAccepted, state.AIProcessing)
}

func (a *App) handleAIContentToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	itemID := r.URL.Query().Get("itemId")
	var req AIContentToggleRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "neispravan toggle")
		return
	}
	if err := a.store.SetAIContentEnabled(itemID, req.Enabled); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	state := a.store.State()
	a.decorateAIProcessingState(&state)
	writeJSON(w, http.StatusOK, state)
}

func (a *App) handleCatalogClear(w http.ResponseWriter, r *http.Request) {
	a.aiProcessingMu.Lock()
	a.aiGeneration++
	if a.aiCancel != nil {
		a.aiCancel()
	}
	a.aiContext, a.aiCancel = context.WithCancel(context.Background())
	a.aiProcessingMu.Unlock()
	a.aiQueue.Clear()
	if err := a.store.Clear(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logEvent("catalog", "store cleared", map[string]any{
		"temp_dir": a.store.TempDir(),
	})
	writeJSON(w, http.StatusOK, a.store.State())
}

func (a *App) handleCatalogExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CatalogExportRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "neispravan export payload")
		return
	}
	if pending, failed := a.store.AIWorkBlocking(req.ItemKeys, req.TowKods); pending > 0 || failed > 0 {
		message := fmt.Sprintf("sačekajte da AI završi opise (%d preostalo)", pending)
		if failed > 0 {
			message = fmt.Sprintf("AI opis nije uspio za %d artikala; ponovite obradu ili isključite AI", failed)
		}
		writeError(w, http.StatusConflict, message)
		return
	}

	var archive bytes.Buffer
	fileName, err := a.store.WriteExportZIP(&archive, req.ItemKeys, req.TowKods)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive.Bytes())

	logEvent("catalog", "catalog export generated", map[string]any{
		"file_name": fileName,
		"item_keys": len(req.ItemKeys),
		"tow_kods":  len(req.TowKods),
	})
}

func (a *App) handleClientLogEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "neispravan log payload")
		return
	}

	var event clientLogEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, "neispravan log payload")
		return
	}

	details := map[string]any{
		"client_path":      event.Path,
		"client_timestamp": event.Timestamp,
		"level":            event.Level,
		"remote_addr":      requestClientIP(r),
	}
	for key, value := range event.Details {
		details[key] = value
	}

	logEvent("frontend", firstNonEmpty(event.Category, "client_event")+": "+firstNonEmpty(event.Message, "event"), details)
	writeJSON(w, http.StatusAccepted, map[string]bool{"logged": true})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}
