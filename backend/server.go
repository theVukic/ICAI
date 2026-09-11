package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultHostAlias       = "asi-app.localhost"
	singleInstanceLockAddr = "127.0.0.1:46321"
)

var preferredPorts = []int{8080, 8081, 8082, 8083, 8084, 8090}

type App struct {
	store                *CatalogStore
	server               *http.Server
	listener             net.Listener
	instanceLock         net.Listener
	distDir              string
	runtimeURLPath       string
	baseURL              string
	descriptionDir       string
	olxCategories        *OLXCategoryService
	auth                 *AuthManager
	publishQueue         chan queuedPublishRequest
	imageProcessor       *ImageProcessor
	descriptionProcessor *DescriptionProcessor
	aiProcessingMu       sync.RWMutex
	aiProcessingEnabled  bool
	aiGeneration         uint64
	aiContext            context.Context
	aiCancel             context.CancelFunc
	aiQueue              *aiJobQueue
	shutdownOnce         sync.Once
	desktop              desktopWindow
	update               *updateManager
}

type queuedPublishRequest struct {
	ctx        context.Context
	items      []CatalogItem
	responseCh chan queuedPublishResult
}

type queuedPublishResult struct {
	response CatalogPublishResponse
	status   int
	err      error
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
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

	storagePaths, err := initializeAppStorage(workDir, projectDir, exeDir)
	if err != nil {
		return nil, err
	}

	store, err := NewCatalogStore()
	if err != nil {
		return nil, err
	}
	app := &App{
		store:          store,
		distDir:        distDir,
		runtimeURLPath: filepath.Join(os.TempDir(), "asi-app-runtime-url.txt"),
		descriptionDir: resolveDescriptionDir([]string{
			storagePaths.DescriptionDir,
			filepath.Join(workDir, "description"),
			filepath.Join(projectDir, "description"),
			filepath.Join(exeDir, "description"),
		}),
		olxCategories: NewOLXCategoryService([]string{
			storagePaths.BackendDir,
			storagePaths.RootDir,
			exeDir,
			filepath.Join(exeDir, "backend"),
			projectDir,
			filepath.Join(projectDir, "backend"),
			workDir,
			filepath.Join(workDir, "backend"),
		}),
		auth: NewAuthManager([]string{
			storagePaths.BackendDir,
			storagePaths.RootDir,
			filepath.Join(exeDir, "backend"),
			exeDir,
			filepath.Join(projectDir, "backend"),
			projectDir,
			filepath.Join(workDir, "backend"),
			workDir,
		}),
		publishQueue:         make(chan queuedPublishRequest, 128),
		imageProcessor:       NewImageProcessor(filepath.Join(exeDir, "background"), store.TempDir(), filepath.Join(exeDir, "models")),
		descriptionProcessor: NewDescriptionProcessor(filepath.Join(exeDir, "models"), filepath.Join(storagePaths.RootDir, "ai-description-cache")),
		aiQueue:              newAIJobQueue(),
		aiProcessingEnabled:  true,
	}
	app.update = newUpdateManager(app)
	app.desktop = newDesktopWindow()

	logEvent("startup", "application paths resolved", map[string]any{
		"app_data_root":   storagePaths.RootDir,
		"app_backend_dir": storagePaths.BackendDir,
		"dist_dir":        distDir,
		"description_dir": app.descriptionDir,
		"runtime_url":     app.runtimeURLPath,
		"work_dir":        workDir,
	})

	if app.olxCategories != nil {
		logEvent("startup", "olx token configuration loaded", map[string]any{
			"active_profile":     app.olxCategories.ActiveProfileName(),
			"available_profiles": app.olxCategories.AvailableProfiles(),
			"token_configured":   app.olxCategories.HasToken(),
			"token_preview":      maskSecret(app.olxCategories.token),
		})
	}

	mux := http.NewServeMux()
	app.registerRoutes(mux)

	app.server = &http.Server{
		Handler: app.withHTTPLogging(mux),
	}

	go app.processPublishQueue()
	go app.processAIQueue()

	return app, nil
}

func (a *App) imageProcessingIsEnabled() bool {
	a.aiProcessingMu.RLock()
	defer a.aiProcessingMu.RUnlock()
	return a.aiProcessingEnabled
}

func (a *App) decorateAIProcessingState(state *CatalogState) {
	if state == nil {
		return
	}
	imageReady, imageError := a.imageProcessor.AIStatus()
	descriptionReady, descriptionError := a.descriptionProcessor.Status()
	ready := imageReady && descriptionReady
	reason := strings.TrimSpace(strings.Join([]string{imageError, descriptionError}, "; "))
	state.ImageProcessing.Enabled = a.imageProcessingIsEnabled()
	state.ImageProcessing.AIReady = imageReady
	state.ImageProcessing.AIError = imageError
	state.AIProcessing.Enabled = a.imageProcessingIsEnabled()
	state.AIProcessing.AIReady = ready
	state.AIProcessing.AIError = reason
	for index := range state.Items {
		a.decorateCatalogItem(&state.Items[index])
	}
}

func (a *App) decorateCatalogItem(item *CatalogItem) {
	if item == nil {
		return
	}
	imagePath, progress := a.store.ProcessedImage(item.ItemID)
	if progress.Status == "done" && strings.TrimSpace(imagePath) != "" {
		version := "0"
		if info, err := os.Stat(imagePath); err == nil {
			version = strconv.FormatInt(info.ModTime().UnixNano(), 10)
		}
		item.ProcessedImageURL = "/api/catalog/processed-image?itemId=" + url.QueryEscape(item.ItemID) + "&v=" + version
	}
}

func (a *App) currentAIGeneration() (uint64, bool, context.Context) {
	a.aiProcessingMu.RLock()
	defer a.aiProcessingMu.RUnlock()
	ctx := a.aiContext
	if ctx == nil {
		ctx = context.Background()
	}
	return a.aiGeneration, a.aiProcessingEnabled, ctx
}

func (a *App) isCurrentAIJob(job aiCatalogJob) bool {
	a.aiProcessingMu.RLock()
	defer a.aiProcessingMu.RUnlock()
	return a.aiProcessingEnabled && a.aiGeneration == job.Generation
}

func (a *App) enqueueAISelection(itemKeys, towKods []string, kind string) error {
	targets := newCatalogSelectionTargets(itemKeys, towKods)
	if targets.Empty() {
		return fmt.Errorf("odaberi barem jedan artikal")
	}
	generation, enabled, _ := a.currentAIGeneration()
	if !enabled {
		return fmt.Errorf("AI nije dostupan")
	}
	count := 0
	for _, item := range a.store.State().Items {
		if !targets.Match(item) {
			continue
		}
		if kind == "image" {
			_, progress := a.store.ProcessedImage(item.ItemID)
			if progress.Status == "done" || progress.Status == "queued" || progress.Status == "processing" {
				continue
			}
			a.store.SetImageProcessingStatus(item.ItemID, ImageProcessingItem{Status: "queued"})
		} else {
			progress := a.store.DescriptionProcessing(item.ItemID)
			if progress.Status == "done" || progress.Status == "queued" || progress.Status == "processing" {
				continue
			}
			a.store.SetDescriptionProcessingStatus(item.ItemID, DescriptionProcessingItem{Status: "queued"})
		}
		// The switch represents the user's chosen output version, including while
		// the requested AI result is still being prepared.
		_ = a.store.SetAIContentEnabled(item.ItemID, true)
		a.aiQueue.Push(aiCatalogJob{ItemID: item.ItemID, Generation: generation, Kind: kind})
		count++
	}
	if count == 0 {
		return nil
	}
	return nil
}

func (a *App) processAIQueue() {
	for {
		job, ok := a.aiQueue.Pop()
		if !ok {
			return
		}
		if !a.isCurrentAIJob(job) {
			continue
		}
		item, err := a.store.GetItemByID(job.ItemID)
		if err != nil {
			continue
		}
		if job.Kind == "image" {
			_, imageProgress := a.store.ProcessedImage(item.ItemID)
			if imageProgress.Status != "done" {
				a.store.SetImageProcessingStatus(item.ItemID, ImageProcessingItem{Status: "processing"})
				if len(item.Images) == 0 {
					a.store.SetImageProcessingStatus(item.ItemID, ImageProcessingItem{Status: "error", Error: "artikal nema sliku"})
				} else {
					result, model, imageErr := a.imageProcessor.Compose(item.ItemID, item.Images[0], func(selectedModel string) {
						a.store.SetImageProcessingStatus(item.ItemID, ImageProcessingItem{Status: "processing", Model: selectedModel})
					})
					if !a.isCurrentAIJob(job) {
						if result != "" {
							_ = os.Remove(result)
						}
						continue
					}
					if imageErr != nil {
						a.store.SetImageProcessingStatus(item.ItemID, ImageProcessingItem{Status: "error", Error: imageErr.Error()})
						logEvent("ai-image", "image processing failed", map[string]any{
							"item_id": item.ItemID,
							"error":   imageErr.Error(),
						})
					} else {
						a.store.SetProcessedImageWithModel(item.ItemID, result, model)
					}
				}
			}

		}
		if job.Kind != "description" || !a.isCurrentAIJob(job) {
			continue
		}
		if a.store.DescriptionProcessing(item.ItemID).Status != "done" {
			a.store.SetDescriptionProcessingStatus(item.ItemID, DescriptionProcessingItem{Status: "processing", Model: descriptionModelName})
			_, _, descriptionCtx := a.currentAIGeneration()
			description, source, descriptionErr := a.descriptionProcessor.Generate(descriptionCtx, item)
			if !a.isCurrentAIJob(job) {
				continue
			}
			if descriptionErr != nil {
				a.store.SetDescriptionProcessingStatus(item.ItemID, DescriptionProcessingItem{Status: "error", Error: descriptionErr.Error(), Model: descriptionModelName})
				logEvent("ai-description", "description generation failed", map[string]any{
					"item_id": item.ItemID,
					"error":   descriptionErr.Error(),
				})
				continue
			}
			if err := a.store.SetGeneratedDescription(item.ItemID, description, source, descriptionModelName); err != nil {
				a.store.SetDescriptionProcessingStatus(item.ItemID, DescriptionProcessingItem{Status: "error", Error: err.Error(), Model: descriptionModelName})
			}
		}
	}
}

func (a *App) processPublishQueue() {
	for req := range a.publishQueue {
		result := queuedPublishResult{}

		if err := req.ctx.Err(); err != nil {
			result.status = http.StatusRequestTimeout
			result.err = fmt.Errorf("publish zahtjev je otkazan prije obrade: %w", err)
		} else {
			result.response, result.status, result.err = a.publishCatalogItems(req.ctx, req.items)
		}

		req.responseCh <- result
	}
}

func (a *App) Run() error {
	defer func() {
		a.aiQueue.Close()
		a.descriptionProcessor.Stop()
		if err := a.store.Cleanup(); err != nil {
			log.Printf("cleanup temp foldera nije uspeo: %v", err)
		}
	}()

	if err := a.acquireSingleInstanceLock(); err != nil {
		existingURL := a.readRuntimeURL()
		logEvent("startup", "single instance already running", map[string]any{
			"existing_url": existingURL,
		})
		focusExistingASIWindow(existingURL)
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

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- a.server.Serve(a.listener)
	}()
	go a.waitForShutdown()
	go a.update.checkOnce(context.Background())

	if a.olxCategories != nil && a.olxCategories.HasToken() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		if err := a.olxCategories.LoadMe(ctx); err != nil {
			log.Printf("OLX /me nije uspeo: %v", err)
		} else {
			session := a.olxCategories.SessionInfo()
			log.Printf("Prijavljen si kao %s", session.Username)
			logEvent("startup", "olx session loaded", map[string]any{
				"profile_name": session.ProfileName,
				"username":     session.Username,
				"full_name":    session.FullName,
			})
		}
	}

	log.Printf("App running on %s", a.baseURL)
	log.Printf("Temp folder: %s", a.store.TempDir())

	if err := a.desktop.Run(a.baseURL); err != nil {
		a.requestShutdown()
		return err
	}

	a.requestShutdown()
	err = <-serverDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/test", a.handleHealth)
	mux.HandleFunc("/api/logs/event", a.handleClientLogEvent)
	mux.HandleFunc("/api/update/status", a.handleUpdateStatus)
	mux.HandleFunc("/api/update/accept", a.handleUpdateAccept)
	mux.HandleFunc("/api/update/decline", a.handleUpdateDecline)

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
	protectedAPI.HandleFunc("/api/catalog/processed-image", a.handleProcessedCatalogImage)
	protectedAPI.HandleFunc("/api/ai-processing/run", a.handleAIProcess)
	protectedAPI.HandleFunc("/api/ai-processing/content", a.handleAIContentToggle)

	protectedAPI.HandleFunc("/api/catalog/bulk-delete", a.handleCatalogBulkDelete)
	protectedAPI.HandleFunc("/api/catalog/bulk-edit", a.handleCatalogBulkEdit)
	protectedAPI.HandleFunc("/api/catalog/export", a.handleCatalogExport)
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

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written, err := w.ResponseWriter.Write(body)
	w.bytes += written
	return written, err
}

func (a *App) withHTTPLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}

		authenticated := false
		appUsername := ""
		if a.auth != nil {
			if session, ok := a.auth.SessionFromRequest(r); ok {
				authenticated = true
				appUsername = session.Username
			}
		}

		next.ServeHTTP(recorder, r)

		if shouldSkipRoutineHTTPLog(r.URL.Path, r.Method, recorder.status) {
			return
		}

		logEvent("http", "request completed", map[string]any{
			"app_username":  appUsername,
			"authenticated": authenticated,
			"bytes":         recorder.bytes,
			"duration_ms":   time.Since(startedAt).Milliseconds(),
			"method":        r.Method,
			"path":          r.URL.Path,
			"query":         r.URL.RawQuery,
			"remote_addr":   requestClientIP(r),
			"status":        recorder.status,
			"user_agent":    r.UserAgent(),
		})
	})
}

func shouldSkipRoutineHTTPLog(path string, method string, status int) bool {
	if status >= http.StatusBadRequest {
		return false
	}

	if method == http.MethodPost && path == "/api/logs/event" {
		return true
	}

	if method == http.MethodGet && path == "/api/session" {
		return true
	}

	return false
}

func (a *App) waitForShutdown() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	a.requestShutdown()
}

func (a *App) requestShutdown() {
	a.shutdownOnce.Do(func() {
		if a.desktop != nil {
			a.desktop.Close()
		}
		if a.aiCancel != nil {
			a.aiCancel()
		}
		// llama-server.exe is a hidden child process. Stop it before the HTTP
		// server is torn down so closing ASI never leaves AI running.
		if a.descriptionProcessor != nil {
			a.descriptionProcessor.Stop()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if a.server != nil {
			if err := a.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("gašenje servera nije uspelo: %v", err)
			}
		}
	})
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
