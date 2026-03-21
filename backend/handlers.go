package main

import (
	"encoding/json"
	"net/http"
)

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "BACKEND RADI.",
	})
}

func (a *App) handleCatalogState(w http.ResponseWriter, r *http.Request) {
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

	writeJSON(w, http.StatusCreated, a.store.State())
}

func (a *App) handleCatalogClear(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Clear(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, a.store.State())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}
