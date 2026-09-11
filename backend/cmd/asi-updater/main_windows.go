//go:build windows

// ASI-Updater deliberately has no user interface. It is a short-lived bridge:
// it waits for the old ASI process, downloads the release-specific Inno Setup
// updater and starts it. The user therefore sees the standard Inno UI only.
package main

import (
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
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

type setupAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	UpdateSetup setupAsset `json:"updateSetup"`
}

func main() {
	manifestURL := flag.String("manifest", "", "update manifest URL")
	appDir := flag.String("app-dir", "", "ASI installation directory")
	pid := flag.Int("pid", 0, "ASI process id")
	flag.Parse()
	if strings.TrimSpace(*manifestURL) == "" || strings.TrimSpace(*appDir) == "" {
		return
	}

	// ASI exits immediately after starting this helper. Do not show a custom
	// window while it happens: the next visible window is native Inno Setup.
	waitForProcess(*pid)
	if err := startInnoUpdater(*manifestURL, *appDir); err != nil {
		showError(err)
	}
}

func startInnoUpdater(manifestURL, appDir string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(manifestURL)
	if err != nil {
		return fmt.Errorf("ne mogu preuzeti update plan: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update servis vraća status %d", resp.StatusCode)
	}
	var plan manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&plan); err != nil {
		return errors.New("update manifest nije validan")
	}
	if strings.TrimSpace(plan.UpdateSetup.URL) == "" || strings.TrimSpace(plan.UpdateSetup.SHA256) == "" {
		return errors.New("release nema Inno Setup updater; preuzmite ASI-Setup iz releasea")
	}

	staging, err := os.MkdirTemp("", "asi-inno-update-")
	if err != nil {
		return err
	}
	setup := filepath.Join(staging, "ASI-Update-Setup.exe")
	if err := download(plan.UpdateSetup.URL, setup); err != nil {
		return fmt.Errorf("ne mogu preuzeti Inno updater: %w", err)
	}
	if fileSHA256(setup) != strings.ToLower(strings.TrimSpace(plan.UpdateSetup.SHA256)) {
		return errors.New("SHA-256 provjera Inno updatera nije uspjela")
	}

	cmd := exec.Command(setup, "/DIR="+appDir)
	cmd.Dir = appDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ne mogu pokrenuti Inno updater: %w", err)
	}
	return nil
}

func download(url, target string) error {
	resp, err := (&http.Client{Timeout: 15 * time.Minute}).Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download status %d", resp.StatusCode)
	}
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func fileSHA256(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func waitForProcess(pid int) {
	if pid <= 0 {
		return
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	const timeout = 8 * time.Second
	const waitTimeout = 258
	if result, _ := windows.WaitForSingleObject(handle, uint32(timeout.Milliseconds())); result == waitTimeout {
		// Never use /T: the bridge was started by ASI and would be killed too.
		command := exec.Command("taskkill", "/PID", fmt.Sprint(pid), "/F")
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = command.Run()
		_, _ = windows.WaitForSingleObject(handle, 5_000)
	}
}

func showError(err error) {
	command := exec.Command("mshta.exe", "javascript:alert("+jsString("ASI ažuriranje nije pokrenuto.\n\n"+err.Error()+"\n\nPreuzmite ASI-Setup iz GitHub releasea.")+");close()")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = command.Start()
}

func jsString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
