package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type MessageResponse struct {
	Message string `json:"message"`
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

func main() {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	exeDir := filepath.Dir(exePath)
	distDir := filepath.Join(exeDir, "dist")

	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		log.Fatalf("dist folder nije pronađen: %s", distDir)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(MessageResponse{
			Message: "BACKEND RADI.",
		})
	})

	fileServer := http.FileServer(http.Dir(distDir))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		if path == "" {
			http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
			return
		}

		fullPath := filepath.Join(distDir, filepath.Clean(path))
		if _, err := os.Stat(fullPath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})

	go func() {
		time.Sleep(700 * time.Millisecond)
		openBrowser("http://localhost:8080")
	}()

	log.Println("App running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
