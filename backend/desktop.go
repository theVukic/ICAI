package main

// desktopWindow is intentionally small: the HTTP server remains the source of
// truth and this window owns only the local WebView2 surface.
type desktopWindow interface {
	Run(url string) error
	Close()
}
