package main

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"
)

func TestU2NetAssetsLoadAndProduceMask(t *testing.T) {
	assets := filepath.Join("assets", "image-ai")
	remover := NewAIBackgroundRemover(filepath.Join(assets, "u2net.onnx"), filepath.Join(assets, "onnxruntime.dll"))
	if ready, reason := remover.Status(); !ready {
		t.Fatalf("local AI should initialize from bundled assets: %s", reason)
	}
	input := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			input.SetRGBA(x, y, color.RGBA{245, 245, 245, 255})
		}
	}
	for y := 20; y < 62; y++ {
		for x := 24; x < 58; x++ {
			input.SetRGBA(x, y, color.RGBA{30, 40, 55, 255})
		}
	}
	mask, err := remover.Mask(input)
	if err != nil {
		t.Fatalf("AI mask failed: %v", err)
	}
	if got, want := mask.Bounds().Size(), input.Bounds().Size(); got != want {
		t.Fatalf("mask size %v, want %v", got, want)
	}
}

func TestBiRefNetAssetsLoadAndProduceMask(t *testing.T) {
	if testing.Short() {
		t.Skip("BiRefNet test loads the bundled high-quality model")
	}
	assets := filepath.Join("assets", "image-ai")
	fast := NewAIBackgroundRemover(filepath.Join(assets, "u2net.onnx"), filepath.Join(assets, "onnxruntime.dll"))
	if ready, reason := fast.Status(); !ready {
		t.Fatalf("ONNX runtime should initialize: %s", reason)
	}
	input := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			input.SetRGBA(x, y, color.RGBA{235, 225, 210, 255})
		}
	}
	for y := 15; y < 52; y++ {
		for x := 19; x < 48; x++ {
			input.SetRGBA(x, y, color.RGBA{25, 35, 55, 255})
		}
	}
	mask, err := NewDetailedAIBackgroundRemover(filepath.Join(assets, "birefnet-general.onnx")).Mask(input)
	if err != nil {
		t.Fatalf("BiRefNet mask failed: %v", err)
	}
	if got, want := mask.Bounds().Size(), input.Bounds().Size(); got != want {
		t.Fatalf("mask size %v, want %v", got, want)
	}
}

func TestDetailedSegmentationSelection(t *testing.T) {
	plain := image.NewRGBA(image.Rect(0, 0, 80, 80))
	complex := image.NewRGBA(image.Rect(0, 0, 80, 80))
	mask := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			plain.SetRGBA(x, y, color.RGBA{250, 250, 250, 255})
			complex.SetRGBA(x, y, color.RGBA{25, 95, 185, 255})
			mask.SetRGBA(x, y, color.RGBA{255, 255, 255, 0})
		}
	}
	if needsDetailedSegmentation(plain, mask) {
		t.Fatal("plain studio image should use the fast model")
	}
	if !needsDetailedSegmentation(complex, mask) {
		t.Fatal("coloured scene should use the detailed model")
	}
}
