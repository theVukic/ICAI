package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type App struct {
	store          *CatalogStore
	server         *http.Server
	distDir        string
	descriptionDir string
	olxCategories  *OLXCategoryService
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
		descriptionDir: resolveDescriptionDir([]string{filepath.Join(workDir, "description"), filepath.Join(projectDir, "description"), filepath.Join(exeDir, "description")}),
		olxCategories: NewOLXCategoryService([]string{
			exeDir,
			filepath.Join(exeDir, "backend"),
			projectDir,
			filepath.Join(projectDir, "backend"),
			workDir,
			filepath.Join(workDir, "backend"),
		}),
	}

	mux := http.NewServeMux()
	app.registerRoutes(mux)

	app.server = &http.Server{
		Addr:    ":8080",
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

	go a.waitForShutdown()
	go func() {
		time.Sleep(700 * time.Millisecond)
		openBrowser("http://localhost:8080")
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

	log.Printf("App running on http://localhost:8080")
	log.Printf("Temp folder: %s", a.store.TempDir())

	err := a.server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/test", a.handleHealth)
	mux.HandleFunc("/api/catalog", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			a.handleCatalogState(w, r)
		case http.MethodDelete:
			a.handleCatalogClear(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/catalog/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleCatalogUpload(w, r)
	})

	mux.HandleFunc("/api/catalog/bulk-delete", a.handleCatalogBulkDelete)
	mux.HandleFunc("/api/catalog/bulk-edit", a.handleCatalogBulkEdit)
	mux.HandleFunc("/api/catalog/publish", a.handleCatalogPublish)
	mux.HandleFunc("/api/catalog/item/", a.handleCatalogItem)
	mux.HandleFunc("/api/description/render/", a.handleDescriptionRender)
	mux.HandleFunc("/api/description", a.handleDescription)
	mux.HandleFunc("/api/category/bulk", a.handleCategoryBulk)
	mux.HandleFunc("/api/category/item/", a.handleCategoryItem)
	mux.HandleFunc("/api/olx/", a.handleOLXCategories)
	mux.HandleFunc("/api/session", a.handleSessionInfo)

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
