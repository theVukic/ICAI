//go:build !windows

package main

type browserDesktop struct{}

func newDesktopWindow() desktopWindow       { return browserDesktop{} }
func (browserDesktop) Run(url string) error { openBrowser(url); select {} }
func (browserDesktop) Close()               {}
func focusExistingASIWindow(url string)     { openBrowser(url) }
