package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultHostAlias       = "icai-app.localhost"
	singleInstanceLockAddr = "127.0.0.1:46321"
)

var preferredPorts = []int{8080, 8081, 8082, 8083, 8084, 8090}

type App struct {
	store          *CatalogStore
	server         *http.Server
	listener       net.Listener
	instanceLock   net.Listener
	distDir        string
	runtimeURLPath string
	baseURL        string
	descriptionDir string
	olxCategories  *OLXCategoryService
	auth           *AuthManager
}

func NewApp() (*App, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	exeDir := filepath.Dir(exePath)
	distDir := filepath.Join(exeDir, "dist")
	workDir, _ := os.Getwd()
	projectDir := filepath.Dir(exeDir)

	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		return nil, errors.New("dist folder nije pronađen: " + distDir)
	}

	store, err := NewCatalogStore()
	if err != nil {
		return nil, err
	}

	app := &App{
		store:          store,
		distDir:        distDir,
		runtimeURLPath: filepath.Join(os.TempDir(), "icai-app-runtime-url.txt"),
		descriptionDir: resolveDescriptionDir([]string{filepath.Join(workDir, "description"), filepath.Join(projectDir, "description"), filepath.Join(exeDir, "description")}),
		olxCategories: NewOLXCategoryService([]string{
			exeDir,
			filepath.Join(exeDir, "backend"),
			projectDir,
			filepath.Join(projectDir, "backend"),
			workDir,
			filepath.Join(workDir, "backend"),
		}),
		auth: NewAuthManager([]string{
			filepath.Join(exeDir, "backend"),
			exeDir,
			filepath.Join(projectDir, "backend"),
			projectDir,
			filepath.Join(workDir, "backend"),
			workDir,
		}),
	}

	mux := http.NewServeMux()
	app.registerRoutes(mux)

	app.server = &http.Server{
		Handler: mux,
	}

	return app, nil
}

func (a *App) Run() error {
	defer func() {
		if err := a.store.Cleanup(); err != nil {
			log.Printf("cleanup temp foldera nije uspeo: %v", err)
		}
	}()

	if err := a.acquireSingleInstanceLock(); err != nil {
		existingURL := a.readRuntimeURL()
		if existingURL != "" {
			openBrowser(existingURL)
		}
		return nil
	}
	defer a.releaseSingleInstanceLock()

	listener, port, err := findAvailableHTTPListener(preferredPorts)
	if err != nil {
		return err
	}
	a.listener = listener
	a.baseURL = fmt.Sprintf("http://%s:%d", defaultHostAlias, port)
	a.persistRuntimeURL(a.baseURL)
	defer a.clearRuntimeURL()

	go a.waitForShutdown()
	go func() {
		time.Sleep(700 * time.Millisecond)
		openBrowser(a.baseURL)
	}()

	if a.olxCategories != nil && a.olxCategories.HasToken() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		if err := a.olxCategories.LoadMe(ctx); err != nil {
			log.Printf("OLX /me nije uspeo: %v", err)
		} else {
			session := a.olxCategories.SessionInfo()
			log.Printf("Prijavljen si kao %s", session.Username)
		}
	}

	log.Printf("App running on %s", a.baseURL)
	log.Printf("Temp folder: %s", a.store.TempDir())

	err = a.server.Serve(a.listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/test", a.handleHealth)

	protectedAPI := http.NewServeMux()

	protectedAPI.HandleFunc("/api/catalog", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			a.handleCatalogState(w, r)
		case http.MethodDelete:
			a.handleCatalogClear(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	protectedAPI.HandleFunc("/api/catalog/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleCatalogUpload(w, r)
	})

	protectedAPI.HandleFunc("/api/catalog/bulk-delete", a.handleCatalogBulkDelete)
	protectedAPI.HandleFunc("/api/catalog/bulk-edit", a.handleCatalogBulkEdit)
	protectedAPI.HandleFunc("/api/catalog/publish", a.handleCatalogPublish)
	protectedAPI.HandleFunc("/api/catalog/item/", a.handleCatalogItem)
	protectedAPI.HandleFunc("/api/description/render/", a.handleDescriptionRender)
	protectedAPI.HandleFunc("/api/description", a.handleDescription)
	protectedAPI.HandleFunc("/api/category/bulk", a.handleCategoryBulk)
	protectedAPI.HandleFunc("/api/category/item/", a.handleCategoryItem)
	protectedAPI.HandleFunc("/api/olx/", a.handleOLXCategories)
	protectedAPI.HandleFunc("/api/listing-limits", a.handleListingLimits)

	mux.HandleFunc("/api/login", a.handleLogin)
	mux.HandleFunc("/api/logout", a.handleLogout)
	mux.HandleFunc("/api/session", a.handleSessionInfo)
	mux.Handle("/api/", a.requireAuthentication(protectedAPI))

	fileServer := http.FileServer(http.Dir(a.distDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			w.Header().Set("Clear-Site-Data", "\"cache\"")
			http.ServeFile(w, r, filepath.Join(a.distDir, "index.html"))
			return
		}

		fullPath := filepath.Join(a.distDir, filepath.Clean(path))
		if _, err := os.Stat(fullPath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, filepath.Join(a.distDir, "index.html"))
	})
}

func (a *App) waitForShutdown() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.server.Shutdown(ctx); err != nil {
		log.Printf("gašenje servera nije uspelo: %v", err)
	}
}

func (a *App) acquireSingleInstanceLock() error {
	lock, err := net.Listen("tcp", singleInstanceLockAddr)
	if err != nil {
		return err
	}

	a.instanceLock = lock
	return nil
}

func (a *App) releaseSingleInstanceLock() {
	if a.instanceLock != nil {
		_ = a.instanceLock.Close()
		a.instanceLock = nil
	}
}

func (a *App) persistRuntimeURL(url string) {
	if strings.TrimSpace(a.runtimeURLPath) == "" || strings.TrimSpace(url) == "" {
		return
	}

	_ = os.WriteFile(a.runtimeURLPath, []byte(url), 0o600)
}

func (a *App) readRuntimeURL() string {
	if strings.TrimSpace(a.runtimeURLPath) == "" {
		return ""
	}

	data, err := os.ReadFile(a.runtimeURLPath)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func (a *App) clearRuntimeURL() {
	if strings.TrimSpace(a.runtimeURLPath) == "" {
		return
	}

	_ = os.Remove(a.runtimeURLPath)
}

func findAvailableHTTPListener(ports []int) (net.Listener, int, error) {
	for _, port := range ports {
		if port <= 0 {
			continue
		}

		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			return listener, port, nil
		}
	}

	return nil, 0, fmt.Errorf("nijedan HTTP port nije slobodan: %v", ports)
}

func openBrowser(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return
	}

	_ = cmd.Start()
}

func resolveDescriptionDir(candidates []string) string {
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}

		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}

		if err := os.MkdirAll(candidate, 0o755); err == nil {
			return candidate
		}
	}

	return filepath.Join(".", "description")
}
