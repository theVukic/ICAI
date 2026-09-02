package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ImageProcessor keeps composition local. The matte extractor is optimized for
// supplier product photos with a plain, light background; it flood-fills only
// from the outer edge so chrome and white product details remain intact.
type ImageProcessor struct {
	backgroundDir string
	outputDir     string
	client        *http.Client
	ai            *AIBackgroundRemover
	detailedAI    *DetailedAIBackgroundRemover
}

func NewImageProcessor(backgroundDir, tempDir, modelDir string) *ImageProcessor {
	// Keep the user-facing asset folder next to ASI.exe available from the
	// first launch, even before the first catalog is imported.
	_ = os.MkdirAll(backgroundDir, 0755)
	return &ImageProcessor{
		backgroundDir: backgroundDir,
		outputDir:     filepath.Join(tempDir, "processed-images"),
		client:        &http.Client{Timeout: 45 * time.Second},
		ai:            NewAIBackgroundRemover(filepath.Join(modelDir, "u2net.onnx"), filepath.Join(modelDir, "onnxruntime.dll")),
		detailedAI:    NewDetailedAIBackgroundRemover(filepath.Join(modelDir, "birefnet-general.onnx")),
	}
}

func (p *ImageProcessor) AIStatus() (bool, string) {
	if ready, reason := p.ai.Status(); !ready {
		return false, reason
	}
	if err := p.detailedAI.Available(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (p *ImageProcessor) Compose(itemID string, source CatalogImage, onModel func(string)) (string, string, error) {
	backgroundPath, err := p.backgroundImage()
	if err != nil {
		return "", "", err
	}
	backgroundFile, err := os.Open(backgroundPath)
	if err != nil {
		return "", "", fmt.Errorf("nije moguće otvoriti pozadinu: %w", err)
	}
	defer backgroundFile.Close()
	background, _, err := image.Decode(backgroundFile)
	if err != nil {
		return "", "", fmt.Errorf("pozadina nije podržana slika: %w", err)
	}
	sourceImage, err := p.load(source)
	if err != nil {
		return "", "", err
	}

	product, mask, bounds, model, err := removeWithAI(sourceImage, p.ai, p.detailedAI, onModel)
	if err != nil {
		return "", "", err
	}
	canvas := resizeToCoverBilinear(background, 1280, 1024)
	product = cropRGBA(product, bounds)
	mask = cropRGBA(mask, bounds)
	// This fills the hero area while keeping the logo on the supplied
	// background unobstructed. The previous smaller preset left too much empty
	// space around wide exhaust tips and similar products.
	product, mask = fitProduct(product, mask, 1280, 900)
	x := (1280 - product.Bounds().Dx()) / 2
	y := 925 - product.Bounds().Dy()
	// A wider feather removes staircase/jagged edges left when an AI mask is
	// enlarged for the final product composition.
	softMask := featherMask(mask, 8)
	product = applyMaskAlpha(product, softMask)
	const effectPad = 155
	const outerGlowSize = 115
	const dropShadowSize = 130
	// Photoshop applies these layer effects to the opaque product silhouette.
	// Do the same here: do not let the low-confidence AI fringe participate in
	// a 100 px blur, because it turns tiny background remnants into a long line.
	// A modest spread gives both effects a clear outline which follows the
	// product contour rather than fading into the background immediately.
	effectMask := padMask(dilateMask(thresholdMask(softMask, 190), 6), effectPad)
	outerGlow := boostEffectAlpha(makeOuterGlow(effectMask, outerGlowSize), 2.4)
	dropShadow := boostEffectAlpha(makeDropShadow(effectMask, dropShadowSize), 2.4)
	// White Drop Shadow, placed just below the product to retain an obvious
	// contour-following effect instead of a detached halo.
	draw.Draw(canvas, image.Rect(x-effectPad, y-effectPad+48, x-effectPad+dropShadow.Bounds().Dx(), y-effectPad+48+dropShadow.Bounds().Dy()), dropShadow, dropShadow.Bounds().Min, draw.Over)
	// White Outer Glow around every edge of the product.
	draw.Draw(canvas, image.Rect(x-effectPad, y-effectPad, x-effectPad+outerGlow.Bounds().Dx(), y-effectPad+outerGlow.Bounds().Dy()), outerGlow, outerGlow.Bounds().Min, draw.Over)
	draw.Draw(canvas, image.Rect(x, y, x+product.Bounds().Dx(), y+product.Bounds().Dy()), product, product.Bounds().Min, draw.Over)
	if err := os.MkdirAll(p.outputDir, 0755); err != nil {
		return "", "", err
	}
	path := filepath.Join(p.outputDir, sanitizeExportName(itemID)+".png")
	file, err := os.Create(path)
	if err != nil {
		return "", "", err
	}
	err = png.Encode(file, canvas)
	closeErr := file.Close()
	if err != nil {
		return "", "", fmt.Errorf("nije moguće sačuvati obrađenu sliku: %w", err)
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	return path, model, nil
}

func (p *ImageProcessor) backgroundImage() (string, error) {
	if err := os.MkdirAll(p.backgroundDir, 0755); err != nil {
		return "", fmt.Errorf("nije moguće kreirati folder background: %w", err)
	}
	entries, err := os.ReadDir(p.backgroundDir)
	if err != nil {
		return "", fmt.Errorf("nije moguće otvoriti folder background: %w", err)
	}
	var images []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
			images = append(images, filepath.Join(p.backgroundDir, entry.Name()))
		}
	}
	if len(images) != 1 {
		return "", fmt.Errorf("folder background mora sadržati tačno jednu JPG ili PNG sliku")
	}
	return images[0], nil
}

func (p *ImageProcessor) load(source CatalogImage) (image.Image, error) {
	if path := strings.TrimSpace(source.Path); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		img, _, err := image.Decode(file)
		if err != nil {
			return nil, fmt.Errorf("izvorna slika nije podržana: %w", err)
		}
		return img, nil
	}
	url := strings.TrimSpace(source.URL)
	if url == "" {
		return nil, fmt.Errorf("artikal nema dostupnu izvornu sliku")
	}
	response, err := p.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("preuzimanje slike nije uspjelo: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("preuzimanje slike nije uspjelo (%d)", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 25<<20))
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("izvorna slika nije podržana: %w", err)
	}
	return img, nil
}

func removePlainBackground(source image.Image) (*image.RGBA, *image.RGBA, image.Rectangle, error) {
	b := source.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 2 || h < 2 {
		return nil, nil, image.Rectangle{}, fmt.Errorf("slika je premala")
	}
	backgroundMask := make([]bool, w*h)
	queue := make([]image.Point, 0, w*2+h*2)
	push := func(x, y int) {
		i := y*w + x
		if !backgroundMask[i] && isBackground(source.At(b.Min.X+x, b.Min.Y+y)) {
			backgroundMask[i] = true
			queue = append(queue, image.Pt(x, y))
		}
	}
	for x := 0; x < w; x++ {
		push(x, 0)
		push(x, h-1)
	}
	for y := 1; y < h-1; y++ {
		push(0, y)
		push(w-1, y)
	}
	for head := 0; head < len(queue); head++ {
		q := queue[head]
		for _, d := range [...]image.Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, ny := q.X+d.X, q.Y+d.Y
			if nx >= 0 && ny >= 0 && nx < w && ny < h {
				push(nx, ny)
			}
		}
	}
	// Keep only the largest connected foreground object. Thin detached strips
	// of a photographed studio background otherwise become visible as lines
	// below the product after scaling.
	foreground := largestForegroundComponent(backgroundMask, w, h)
	product := image.NewRGBA(image.Rect(0, 0, w, h))
	alpha := image.NewRGBA(image.Rect(0, 0, w, h))
	box := image.Rect(w, h, 0, 0)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !foreground[y*w+x] {
				continue
			}
			c := source.At(b.Min.X+x, b.Min.Y+y)
			product.Set(x, y, c)
			alpha.Set(x, y, color.RGBA{255, 255, 255, 255})
			if x < box.Min.X {
				box.Min.X = x
			}
			if y < box.Min.Y {
				box.Min.Y = y
			}
			if x+1 > box.Max.X {
				box.Max.X = x + 1
			}
			if y+1 > box.Max.Y {
				box.Max.Y = y + 1
			}
		}
	}
	if box.Dx() < 8 || box.Dy() < 8 {
		return nil, nil, image.Rectangle{}, fmt.Errorf("proizvod nije pronađen; koristite sliku sa svijetlom, jednoličnom pozadinom")
	}
	return product, alpha, box, nil
}

func largestForegroundComponent(backgroundMask []bool, w, h int) []bool {
	visited := make([]bool, len(backgroundMask))
	best := make([]int, 0)
	for start := range backgroundMask {
		if backgroundMask[start] || visited[start] {
			continue
		}
		component := make([]int, 0, 1024)
		queue := []int{start}
		visited[start] = true
		for head := 0; head < len(queue); head++ {
			index := queue[head]
			component = append(component, index)
			x, y := index%w, index/w
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					next := ny*w + nx
					if !backgroundMask[next] && !visited[next] {
						visited[next] = true
						queue = append(queue, next)
					}
				}
			}
		}
		if len(component) > len(best) {
			best = component
		}
	}
	result := make([]bool, len(backgroundMask))
	for _, index := range best {
		result[index] = true
	}
	return result
}

func isBackground(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r > 54000 && g > 54000 && b > 54000 && max16(r, g, b)-min16(r, g, b) < 5500
}
func max16(a, b, c uint32) uint32 {
	if a > b {
		b = a
	}
	if b > c {
		c = b
	}
	return b
}
func min16(a, b, c uint32) uint32 {
	if a < b {
		b = a
	}
	if b < c {
		c = b
	}
	return b
}
func cropRGBA(src *image.RGBA, rect image.Rectangle) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), src, rect.Min, draw.Src)
	return out
}
func fitProduct(product, mask *image.RGBA, maxW, maxH int) (*image.RGBA, *image.RGBA) {
	w, h := product.Bounds().Dx(), product.Bounds().Dy()
	scale := minFloat(float64(maxW)/float64(w), float64(maxH)/float64(h))
	nw, nh := maxInt(1, int(float64(w)*scale)), maxInt(1, int(float64(h)*scale))
	return resizeRGBABilinear(product, nw, nh), resizeRGBABilinear(mask, nw, nh)
}
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func tintAndBlur(mask *image.RGBA, tint color.RGBA, radius int) *image.RGBA {
	out := image.NewRGBA(mask.Bounds())
	for y := 0; y < mask.Bounds().Dy(); y++ {
		for x := 0; x < mask.Bounds().Dx(); x++ {
			_, _, _, a := mask.At(x, y).RGBA()
			alpha := uint8(uint32(tint.A) * a / 65535)
			// image.RGBA requires premultiplied colour channels. Writing a full
			// white RGB value with a tiny alpha made the blur leak bright/coloured
			// pixels far outside the actual product.
			out.SetRGBA(x, y, color.RGBA{
				uint8(uint16(tint.R) * uint16(alpha) / 255),
				uint8(uint16(tint.G) * uint16(alpha) / 255),
				uint8(uint16(tint.B) * uint16(alpha) / 255),
				alpha,
			})
		}
	}
	return blurRGBA(out, radius)
}

func makeOuterGlow(mask *image.RGBA, size int) *image.RGBA {
	return tintAndBlur(mask, color.RGBA{255, 255, 255, 255}, size)
}

func makeDropShadow(mask *image.RGBA, size int) *image.RGBA {
	return tintAndBlur(mask, color.RGBA{255, 255, 255, 255}, size)
}

// The requested 100 px effects are intentionally broad. Boost their alpha
// after blurring so the white halo remains clearly visible on dark branding
// backgrounds, while channels stay premultiplied and cannot create artefacts.
func boostEffectAlpha(src *image.RGBA, multiplier float64) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			offset := src.PixOffset(x, y)
			for channel := 0; channel < 4; channel++ {
				value := int(float64(src.Pix[offset+channel])*multiplier + .5)
				if value > 255 {
					value = 255
				}
				out.Pix[offset+channel] = uint8(value)
			}
		}
	}
	return out
}

func padMask(mask *image.RGBA, padding int) *image.RGBA {
	b := mask.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx()+padding*2, b.Dy()+padding*2))
	draw.Draw(out, image.Rect(padding, padding, padding+b.Dx(), padding+b.Dy()), mask, b.Min, draw.Src)
	return out
}

func thresholdMask(mask *image.RGBA, threshold uint8) *image.RGBA {
	out := image.NewRGBA(mask.Bounds())
	for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
		for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
			if mask.RGBAAt(x, y).A >= threshold && hasVerticalMaskSupport(mask, x, y, threshold, 12) {
				out.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	return out
}

// Effects use a stable silhouette, not a faint horizontal tail left by a
// segmentation model. Requiring 25 pixels of vertical support removes such
// tails while retaining the solid body of automotive products.
func hasVerticalMaskSupport(mask *image.RGBA, x, y int, threshold uint8, radius int) bool {
	b := mask.Bounds()
	for dy := -radius; dy <= radius; dy++ {
		row := y + dy
		if row < b.Min.Y || row >= b.Max.Y || mask.RGBAAt(x, row).A < threshold {
			return false
		}
	}
	return true
}

// A sliding-window separable box blur keeps the exact 100 px Photoshop-style
// effect responsive. The older generic blur is O(radius) per pixel and becomes
// unnecessarily expensive at this requested size.
func blurRGBAFast(src *image.RGBA, radius int) *image.RGBA {
	if radius <= 0 {
		return src
	}
	b := src.Bounds()
	width, height := b.Dx(), b.Dy()
	window := radius*2 + 1
	tmp := image.NewRGBA(b)
	out := image.NewRGBA(b)
	for y := 0; y < height; y++ {
		var sums [4]int
		for sample := -radius; sample <= radius; sample++ {
			offset := src.PixOffset(clampInt(sample, 0, width-1), y)
			for channel := 0; channel < 4; channel++ {
				sums[channel] += int(src.Pix[offset+channel])
			}
		}
		for x := 0; x < width; x++ {
			offset := tmp.PixOffset(x, y)
			for channel := 0; channel < 4; channel++ {
				tmp.Pix[offset+channel] = uint8(sums[channel] / window)
			}
			remove := src.PixOffset(clampInt(x-radius, 0, width-1), y)
			add := src.PixOffset(clampInt(x+radius+1, 0, width-1), y)
			for channel := 0; channel < 4; channel++ {
				sums[channel] += int(src.Pix[add+channel]) - int(src.Pix[remove+channel])
			}
		}
	}
	for x := 0; x < width; x++ {
		var sums [4]int
		for sample := -radius; sample <= radius; sample++ {
			offset := tmp.PixOffset(x, clampInt(sample, 0, height-1))
			for channel := 0; channel < 4; channel++ {
				sums[channel] += int(tmp.Pix[offset+channel])
			}
		}
		for y := 0; y < height; y++ {
			offset := out.PixOffset(x, y)
			for channel := 0; channel < 4; channel++ {
				out.Pix[offset+channel] = uint8(sums[channel] / window)
			}
			remove := tmp.PixOffset(x, clampInt(y-radius, 0, height-1))
			add := tmp.PixOffset(x, clampInt(y+radius+1, 0, height-1))
			for channel := 0; channel < 4; channel++ {
				sums[channel] += int(tmp.Pix[add+channel]) - int(tmp.Pix[remove+channel])
			}
		}
	}
	return out
}

func dilateMask(mask *image.RGBA, radius int) *image.RGBA {
	out := image.NewRGBA(mask.Bounds())
	b := mask.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var maximum uint8
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					nx := clampInt(x+dx, b.Min.X, b.Max.X-1)
					ny := clampInt(y+dy, b.Min.Y, b.Max.Y-1)
					if alpha := mask.RGBAAt(nx, ny).A; alpha > maximum {
						maximum = alpha
					}
				}
			}
			out.SetRGBA(x, y, color.RGBA{255, 255, 255, maximum})
		}
	}
	return out
}

func featherMask(mask *image.RGBA, radius int) *image.RGBA { return blurRGBA(mask, radius) }

func applyMaskAlpha(product, mask *image.RGBA) *image.RGBA {
	out := image.NewRGBA(product.Bounds())
	for y := product.Bounds().Min.Y; y < product.Bounds().Max.Y; y++ {
		for x := product.Bounds().Min.X; x < product.Bounds().Max.X; x++ {
			offset := product.PixOffset(x, y)
			_, _, _, alpha := mask.At(x, y).RGBA()
			// image.RGBA stores premultiplied alpha. Keeping full RGB values with
			// a partial alpha produced the dark, sharp contour seen in the output.
			alpha8 := uint8(alpha >> 8)
			out.Pix[offset] = uint8(uint16(product.Pix[offset]) * uint16(alpha8) / 255)
			out.Pix[offset+1] = uint8(uint16(product.Pix[offset+1]) * uint16(alpha8) / 255)
			out.Pix[offset+2] = uint8(uint16(product.Pix[offset+2]) * uint16(alpha8) / 255)
			out.Pix[offset+3] = alpha8
		}
	}
	return out
}

func resizeToCoverBilinear(src image.Image, targetWidth, targetHeight int) *image.RGBA {
	bounds := src.Bounds()
	scaleX := float64(targetWidth) / float64(bounds.Dx())
	scaleY := float64(targetHeight) / float64(bounds.Dy())
	scale := scaleX
	if scaleY > scale {
		scale = scaleY
	}
	scaled := resizeRGBABilinear(src, maxInt(1, int(float64(bounds.Dx())*scale+0.5)), maxInt(1, int(float64(bounds.Dy())*scale+0.5)))
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.Draw(dst, dst.Bounds(), scaled, image.Pt((scaled.Bounds().Dx()-targetWidth)/2, (scaled.Bounds().Dy()-targetHeight)/2), draw.Src)
	return dst
}

func resizeRGBABilinear(src image.Image, targetWidth, targetHeight int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	for y := 0; y < targetHeight; y++ {
		fy := (float64(y)+.5)*float64(sh)/float64(targetHeight) - .5
		y0 := int(fy)
		if y0 < 0 {
			y0 = 0
		}
		y1 := y0 + 1
		if y1 >= sh {
			y1 = sh - 1
		}
		wy := fy - float64(y0)
		if wy < 0 {
			wy = 0
		}
		for x := 0; x < targetWidth; x++ {
			fx := (float64(x)+.5)*float64(sw)/float64(targetWidth) - .5
			x0 := int(fx)
			if x0 < 0 {
				x0 = 0
			}
			x1 := x0 + 1
			if x1 >= sw {
				x1 = sw - 1
			}
			wx := fx - float64(x0)
			if wx < 0 {
				wx = 0
			}
			c00 := color.RGBAModel.Convert(src.At(b.Min.X+x0, b.Min.Y+y0)).(color.RGBA)
			c10 := color.RGBAModel.Convert(src.At(b.Min.X+x1, b.Min.Y+y0)).(color.RGBA)
			c01 := color.RGBAModel.Convert(src.At(b.Min.X+x0, b.Min.Y+y1)).(color.RGBA)
			c11 := color.RGBAModel.Convert(src.At(b.Min.X+x1, b.Min.Y+y1)).(color.RGBA)
			dst.SetRGBA(x, y, color.RGBA{bilinearByte(c00.R, c10.R, c01.R, c11.R, wx, wy), bilinearByte(c00.G, c10.G, c01.G, c11.G, wx, wy), bilinearByte(c00.B, c10.B, c01.B, c11.B, wx, wy), bilinearByte(c00.A, c10.A, c01.A, c11.A, wx, wy)})
		}
	}
	return dst
}

func bilinearByte(a, b, c, d uint8, x, y float64) uint8 {
	top := float64(a)*(1-x) + float64(b)*x
	bottom := float64(c)*(1-x) + float64(d)*x
	return uint8(top*(1-y) + bottom*y + .5)
}
