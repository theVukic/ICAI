package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

//go:embed all:dist
var distFS embed.FS

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
	mux := http.NewServeMux()

	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(MessageResponse{
			Message: "BACKEND RADI.",
		})
	})

	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		log.Fatal(err)
	}

	fileServer := http.FileServer(http.FS(dist))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		if path == "" {
			http.ServeFileFS(w, r, dist, "index.html")
			return
		}

		_, err := fs.Stat(dist, path)
		if err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFileFS(w, r, dist, "index.html")
	})

	go func() {
		time.Sleep(700 * time.Millisecond)
		openBrowser("http://localhost:8080")
	}()

	log.Println("App running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
