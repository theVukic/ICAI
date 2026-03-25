package main

import (
	"encoding/json"
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
	writeJSON(w, http.StatusOK, a.store.State())
}

func (a *App) handleCatalogUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "upload forma nije validna")
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "nije poslat nijedan JSON fajl")
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

	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "nije moguće otvoriti poslati fajl")
			return
		}

		_, saveErr := a.store.AddCatalog(file, header)
		_ = file.Close()
		if saveErr != nil {
			writeError(w, http.StatusBadRequest, saveErr.Error())
			return
		}
	}

	state := a.store.State()
	logEvent("catalog", "upload completed", map[string]any{
		"files_total": state.TotalFiles,
		"items_total": state.TotalItems,
	})
	writeJSON(w, http.StatusCreated, a.store.State())
}

func (a *App) handleCatalogClear(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Clear(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logEvent("catalog", "store cleared", map[string]any{
		"temp_dir": a.store.TempDir(),
	})
	writeJSON(w, http.StatusOK, a.store.State())
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
