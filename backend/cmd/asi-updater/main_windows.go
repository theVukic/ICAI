//go:build windows

// ASI-Updater runs from a temporary copy so it can replace the installed
// updater too. It deliberately accepts only known component IDs and never
// trusts a release manifest to select arbitrary filesystem paths.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

type component struct {
	ID, URL, SHA256 string
	Size            int64
}
type manifest struct {
	Version    string      `json:"version"`
	Notes      string      `json:"notes"`
	Components []component `json:"components"`
}

func main() {
	manifestURL := flag.String("manifest", "", "signed update manifest URL")
	appDir := flag.String("app-dir", "", "ASI installation directory")
	pid := flag.Int("pid", 0, "ASI process id")
	flag.Parse()
	if *manifestURL == "" || *appDir == "" {
		return
	}

	wv := webview2.NewWithOptions(webview2.WebViewOptions{Debug: false, DataPath: filepath.Join(os.TempDir(), "asi-updater-webview"), WindowOptions: webview2.WindowOptions{Title: "ASI ažuriranje", Width: 520, Height: 300, Center: true}})
	wv.SetHtml(`<!doctype html><style>body{margin:0;background:#111820;color:#eaf3ff;font:16px Segoe UI,sans-serif;display:grid;place-items:center;height:100vh}.box{width:390px}.ey{color:#7fbcff;font-size:11px;font-weight:bold;letter-spacing:.12em}.title{font-size:25px;font-weight:700;margin:9px 0}.detail{color:#b6c5d7}.bar{height:10px;background:#263445;border-radius:99px;overflow:hidden;margin:23px 0 10px}.fill{height:100%;width:4%;background:linear-gradient(90deg,#3291ff,#65b4ff);transition:width .2s}.small{color:#8ca0b7;font-size:13px}</style><main class=box><div class=ey>ASI UPDATE</div><div class=title id=title>Pripremam ažuriranje</div><div class=detail id=detail>Čekam da se ASI zatvori…</div><div class=bar><div class=fill id=fill></div></div><div class=small id=small></div></main>`)
	go func() {
		err := runUpdate(*manifestURL, *appDir, *pid, func(title, detail string, percent int) {
			wv.Eval(fmt.Sprintf("document.getElementById('title').textContent=%q;document.getElementById('detail').textContent=%q;document.getElementById('fill').style.width=%q;document.getElementById('small').textContent=%q", title, detail, fmt.Sprintf("%d%%", percent), fmt.Sprintf("%d%%", percent)))
		})
		if err != nil {
			wv.Eval(fmt.Sprintf("document.getElementById('title').textContent='Ažuriranje nije uspjelo';document.getElementById('detail').textContent=%q;document.getElementById('small').textContent='Možete zatvoriti ovaj prozor i nastaviti sa starom verzijom.'", err.Error()))
			return
		}
		wv.Eval("document.getElementById('title').textContent='Ažuriranje je završeno';document.getElementById('detail').textContent='Pokrećem novu verziju ASI-ja…';document.getElementById('fill').style.width='100%'")
		time.Sleep(900 * time.Millisecond)
		_ = exec.Command(filepath.Join(*appDir, "ASI.exe")).Start()
		wv.Terminate()
	}()
	wv.Run()
	wv.Destroy()
}

func runUpdate(manifestURL, appDir string, pid int, progress func(string, string, int)) error {
	if pid > 0 {
		waitForProcess(pid)
	}
	progress("Preuzimam plan", "Provjeravam novu verziju…", 8)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(manifestURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update servis vraća status %d", resp.StatusCode)
	}
	var plan manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&plan); err != nil {
		return errors.New("update manifest nije validan")
	}
	staging, err := os.MkdirTemp("", "asi-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	changed := make([]component, 0)
	for _, item := range plan.Components {
		destination, ok := componentDestination(appDir, item.ID)
		if !ok {
			continue
		}
		if componentInstalled(appDir, destination, item) {
			continue
		}
		changed = append(changed, item)
	}
	for index, item := range changed {
		progress("Preuzimam ažuriranje", item.ID, 10+(index*70/max(1, len(changed))))
		file := filepath.Join(staging, item.ID+filepath.Ext(item.URL))
		if err := download(item.URL, file); err != nil {
			return fmt.Errorf("%s: %w", item.ID, err)
		}
		if item.SHA256 == "" || fileSHA256(file) != strings.ToLower(item.SHA256) {
			return fmt.Errorf("provjera SHA-256 nije uspjela za %s", item.ID)
		}
		if err := installComponent(file, appDir, item.ID); err != nil {
			return err
		}
		if strings.HasSuffix(strings.ToLower(file), ".zip") {
			_ = os.WriteFile(filepath.Join(appDir, ".asi-component-"+item.ID+".sha256"), []byte(strings.ToLower(item.SHA256)), 0o600)
		}
	}
	progress("Provjeravam instalaciju", "Provjera je uspješna.", 95)
	root, err := os.UserConfigDir()
	if err == nil {
		result := struct {
			Version string `json:"version"`
			Notes   string `json:"notes"`
		}{Version: plan.Version, Notes: plan.Notes}
		if encoded, marshalErr := json.Marshal(result); marshalErr == nil {
			_ = os.WriteFile(filepath.Join(root, "ASIapp", "update-result.json"), encoded, 0o600)
		}
	}
	return nil
}

func componentDestination(appDir, id string) (string, bool) {
	switch id {
	case "app":
		return filepath.Join(appDir, "ASI.exe"), true
	case "updater":
		return filepath.Join(appDir, "ASI-Updater.exe"), true
	case "frontend":
		return filepath.Join(appDir, "dist", "index.html"), true
	case "image-model-u2net":
		return filepath.Join(appDir, "models", "u2net.onnx"), true
	case "image-model-birefnet":
		return filepath.Join(appDir, "models", "birefnet-general.onnx"), true
	case "text-model-qwen":
		return filepath.Join(appDir, "models", "Qwen3-4B-Q4_K_M.gguf"), true
	case "runtime":
		return filepath.Join(appDir, "models", "llama-server.exe"), true
	case "extras":
		return filepath.Join(appDir, ".asi-component-extras.sha256"), true
	default:
		return "", false
	}
}
func installComponent(source, appDir, id string) error {
	destination, ok := componentDestination(appDir, id)
	if !ok {
		return nil
	}
	if strings.HasSuffix(strings.ToLower(source), ".zip") {
		target := filepath.Dir(destination)
		if id == "frontend" {
			target = filepath.Join(appDir, "dist")
		}
		if id == "extras" {
			target = appDir
		}
		return extractZip(source, target)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return copy(source, destination)
}

func componentInstalled(appDir, destination string, item component) bool {
	if item.SHA256 == "" {
		return false
	}
	if strings.HasSuffix(strings.ToLower(item.URL), ".zip") {
		marker, err := os.ReadFile(filepath.Join(appDir, ".asi-component-"+item.ID+".sha256"))
		return err == nil && strings.TrimSpace(string(marker)) == strings.ToLower(item.SHA256)
	}
	return fileSHA256(destination) == strings.ToLower(item.SHA256)
}
func extractZip(source, target string) error {
	z, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		path := filepath.Join(target, f.Name)
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(target)+string(os.PathSeparator)) {
			return errors.New("neispravna putanja u paketu")
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		input, err := f.Open()
		if err != nil {
			return err
		}
		output, err := os.Create(path)
		if err == nil {
			_, err = io.Copy(output, input)
			_ = output.Close()
		}
		_ = input.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func download(url, target string) error {
	r, err := (&http.Client{Timeout: 15 * time.Minute}).Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("download status %d", r.StatusCode)
	}
	f, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r.Body)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func copy(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
func waitForProcess(pid int) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err == nil {
		defer windows.CloseHandle(h)
		_, _ = windows.WaitForSingleObject(h, windows.INFINITE)
	}
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
