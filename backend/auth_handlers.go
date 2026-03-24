package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.auth == nil {
		writeError(w, http.StatusInternalServerError, "login servis nije dostupan")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "neispravan login zahtjev")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || strings.TrimSpace(req.Password) == "" {
		writeError(w, http.StatusBadRequest, "username i password su obavezni")
		return
	}

	user, err := a.auth.VerifyLogin(r.Context(), req.Username, req.Password)
	if err != nil {
		a.auth.ClearSessionCookie(w)
		writeJSON(w, http.StatusUnauthorized, LoginResponse{
			Authenticated: false,
			Message:       err.Error(),
		})
		return
	}

	session, err := a.auth.CreateSession(req.Username, user, req.RememberMe)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ne mogu kreirati session")
		return
	}

	a.auth.WriteSessionCookie(w, session)
	writeJSON(w, http.StatusOK, LoginResponse{
		Authenticated: true,
		Username:      session.Username,
		Role:          session.Role,
		Message:       "Login uspjesan.",
	})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.auth != nil {
		a.auth.DeleteSessionFromRequest(r)
		a.auth.ClearSessionCookie(w)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (a *App) requireAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.auth == nil {
			writeError(w, http.StatusInternalServerError, "auth servis nije dostupan")
			return
		}

		if _, ok := a.auth.SessionFromRequest(r); !ok {
			a.auth.ClearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "Morate se prijaviti da biste pristupili aplikaciji.")
			return
		}

		next.ServeHTTP(w, r)
	})
}
