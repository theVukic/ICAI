//go:build windows

package main

import (
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

const asiWindowTitle = "ASI"

type webViewDesktop struct {
	mu sync.Mutex
	wv webview2.WebView
}

func newDesktopWindow() desktopWindow { return &webViewDesktop{} }

func (d *webViewDesktop) Run(url string) error {
	dataRoot, err := resolveAppStorageRoot()
	if err != nil {
		return err
	}
	dataPath := filepath.Join(dataRoot, "webview2")
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		return err
	}
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  dataPath,
		AutoFocus: true,
		// build.ps1 embeds the ASI icon as Windows resource ID 1 through
		// go-winres. Passing it here gives the WebView2 title bar its app icon.
		WindowOptions: webview2.WindowOptions{Title: asiWindowTitle, Width: 1440, Height: 960, IconId: 1, Center: true},
	})
	d.mu.Lock()
	d.wv = wv
	d.mu.Unlock()
	wv.Navigate(url)
	wv.Run()
	wv.Destroy()
	d.mu.Lock()
	d.wv = nil
	d.mu.Unlock()
	return nil
}

func (d *webViewDesktop) Close() {
	d.mu.Lock()
	wv := d.wv
	d.mu.Unlock()
	if wv != nil {
		wv.Terminate()
	}
}

func focusExistingASIWindow(_ string) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowW")
	showWindow := user32.NewProc("ShowWindow")
	setForegroundWindow := user32.NewProc("SetForegroundWindow")
	title, _ := windows.UTF16PtrFromString(asiWindowTitle)
	hwnd, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd != 0 {
		const swRestore = 9
		_, _, _ = showWindow.Call(hwnd, swRestore)
		_, _, _ = setForegroundWindow.Call(hwnd)
	}
}
