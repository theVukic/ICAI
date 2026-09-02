package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ProcessedImageCache survives catalog clearing and application restarts. Its
// filename is derived from the original first product image, so a changed
// supplier image automatically receives a new processed cover.
type ProcessedImageCache struct {
	dir string
	mu  sync.Mutex
}

const processedImagePipelineVersion = "ai-dual-birefnet-v1"

func NewProcessedImageCache(dir string) (*ProcessedImageCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("nije moguće kreirati cache obrađenih slika: %w", err)
	}
	return &ProcessedImageCache{dir: dir}, nil
}

func (c *ProcessedImageCache) Lookup(item CatalogItem) (string, bool) {
	key, err := processedImageCacheKey(item)
	if err != nil {
		return "", false
	}
	path := filepath.Join(c.dir, key+".png")
	info, err := os.Stat(path)
	return path, err == nil && !info.IsDir() && info.Size() > 0
}

func (c *ProcessedImageCache) Save(item CatalogItem, processedPath string) error {
	key, err := processedImageCacheKey(item)
	if err != nil {
		return err
	}
	if strings.TrimSpace(processedPath) == "" {
		return fmt.Errorf("obrađena slika nije dostupna")
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	destination := filepath.Join(c.dir, key+".png")
	c.mu.Lock()
	defer c.mu.Unlock()
	if info, err := os.Stat(destination); err == nil && !info.IsDir() && info.Size() > 0 {
		return nil
	}
	temporary := destination + ".tmp"
	input, err := os.Open(processedPath)
	if err != nil {
		return fmt.Errorf("nije moguće otvoriti obrađenu sliku: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func processedImageCacheKey(item CatalogItem) (string, error) {
	if len(item.Images) == 0 {
		return "", fmt.Errorf("artikal nema izvornu sliku za cache")
	}
	first := item.Images[0]
	hash := sha256.New()
	_, _ = io.WriteString(hash, processedImagePipelineVersion+"\n")
	_, _ = io.WriteString(hash, strings.TrimSpace(item.TowKod)+"\n")
	if path := strings.TrimSpace(first.Path); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	} else if rawURL := strings.TrimSpace(first.URL); rawURL != "" {
		_, _ = io.WriteString(hash, rawURL)
	} else {
		return "", fmt.Errorf("artikal nema dostupnu izvornu sliku za cache")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
