package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AppVersion and UpdateManifestURL are set by release.ps1 using Go ldflags.
// Empty UpdateManifestURL deliberately disables update checks for local builds.
var AppVersion = "0.0.0-dev"
var UpdateManifestURL = ""

type updateComponent struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type updateManifest struct {
	Version    string            `json:"version"`
	Notes      string            `json:"notes"`
	Components []updateComponent `json:"components"`
}

type updateStatus struct {
	Checked        bool   `json:"checked"`
	Available      bool   `json:"available"`
	Updated        bool   `json:"updated"`
	Version        string `json:"version,omitempty"`
	CurrentVersion string `json:"currentVersion"`
	Notes          string `json:"notes,omitempty"`
	Error          string `json:"error,omitempty"`
	Nonce          string `json:"nonce"`
}

type updateManager struct {
	app       *App
	mu        sync.RWMutex
	status    updateStatus
	manifest  updateManifest
	dismissed bool
	checking  sync.Once
}

func newUpdateManager(app *App) *updateManager {
	status := updateStatus{Nonce: randomRuntimeNonce(), CurrentVersion: AppVersion}
	if version, notes := readUpdateResult(); version != "" {
		status.Updated = true
		status.Version = version
		status.Notes = notes
	}
	return &updateManager{app: app, status: status}
}

func (m *updateManager) checkOnce(ctx context.Context) {
	m.checking.Do(func() {
		m.mu.RLock()
		status := updateStatus{
			Checked:        true,
			Nonce:          m.status.Nonce,
			Updated:        m.status.Updated,
			Version:        m.status.Version,
			CurrentVersion: AppVersion,
			Notes:          m.status.Notes,
		}
		m.mu.RUnlock()
		manifestURL := strings.TrimSpace(UpdateManifestURL)
		if manifestURL == "" {
			m.setStatus(status)
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
		if err != nil {
			status.Error = err.Error()
			m.setStatus(status)
			return
		}
		client := &http.Client{Timeout: 12 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			status.Error = err.Error()
			m.setStatus(status)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			status.Error = fmt.Sprintf("update servis vraća status %d", resp.StatusCode)
			m.setStatus(status)
			return
		}
		var manifest updateManifest
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&manifest); err != nil {
			status.Error = "update manifest nije validan"
			m.setStatus(status)
			return
		}
		if !isNewerVersion(manifest.Version, AppVersion) {
			m.setStatus(status)
			return
		}
		status.Available = true
		status.Version = manifest.Version
		status.Notes = manifest.Notes
		m.mu.Lock()
		m.status = status
		m.manifest = manifest
		m.mu.Unlock()
	})
}

type updateResult struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
}

func readUpdateResult() (string, string) {
	root, err := resolveAppStorageRoot()
	if err != nil {
		return "", ""
	}
	path := filepath.Join(root, "update-result.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	_ = os.Remove(path)
	var result updateResult
	if json.Unmarshal(data, &result) != nil {
		return "", ""
	}
	return strings.TrimSpace(result.Version), strings.TrimSpace(result.Notes)
}

func (m *updateManager) setStatus(status updateStatus) {
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
}

func (m *updateManager) currentStatus() updateStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := m.status
	if m.dismissed {
		status.Available = false
	}
	return status
}

func (m *updateManager) dismiss() {
	m.mu.Lock()
	m.dismissed = true
	m.mu.Unlock()
}

func (a *App) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, a.update.currentStatus())
}

func (a *App) handleUpdateDecline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !a.updateRequestAllowed(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	a.update.dismiss()
	writeJSON(w, http.StatusOK, map[string]bool{"dismissed": true})
}

func (a *App) handleUpdateAccept(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !a.updateRequestAllowed(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	a.update.mu.RLock()
	manifest := a.update.manifest
	status := a.update.status
	a.update.mu.RUnlock()
	if !status.Available || strings.TrimSpace(manifest.Version) == "" {
		writeError(w, http.StatusConflict, "update trenutno nije dostupan")
		return
	}
	if err := a.startUpdater(); err != nil {
		writeError(w, http.StatusInternalServerError, "ne mogu pokrenuti updater: "+err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"starting": true})
	go func() {
		time.Sleep(350 * time.Millisecond)
		// First request graceful shutdown so the hidden AI process can stop.
		// If a WebView/HTTP worker keeps the main process alive, force the old
		// ASI process out shortly afterwards; otherwise the updater waits forever.
		go a.requestShutdown()
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()
}

func (a *App) updateRequestAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" && origin != a.baseURL {
		return false
	}
	return r.Header.Get("X-ASI-Update-Nonce") == a.update.currentStatus().Nonce
}

func (a *App) startUpdater() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	appDir := filepath.Dir(exePath)
	installedUpdater := filepath.Join(appDir, "ASI-Updater.exe")
	if _, err := os.Stat(installedUpdater); err != nil {
		return err
	}
	copyPath := filepath.Join(os.TempDir(), "ASI-Updater-"+randomRuntimeNonce()+".exe")
	if err := copyFile(installedUpdater, copyPath, 0o700); err != nil {
		return err
	}
	cmd := exec.Command(copyPath, "-manifest", UpdateManifestURL, "-app-dir", appDir, "-pid", fmt.Sprint(os.Getpid()))
	cmd.Dir = appDir
	return cmd.Start()
}

func randomRuntimeNonce() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}

func isNewerVersion(remote string, local string) bool {
	parse := func(value string) [3]int {
		var result [3]int
		value = strings.TrimPrefix(strings.TrimSpace(value), "v")
		_, _ = fmt.Sscanf(value, "%d.%d.%d", &result[0], &result[1], &result[2])
		return result
	}
	r, l := parse(remote), parse(local)
	for i := range r {
		if r[i] != l[i] {
			return r[i] > l[i]
		}
	}
	return false
}
