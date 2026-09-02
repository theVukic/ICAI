package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

const u2NetInputSize = 320

var onnxEnvironmentOnce sync.Once
var onnxEnvironmentErr error

// AIBackgroundRemover owns one CPU-only U²-Net session. The app deliberately
// uses a single session and one inference worker so importing a catalog does
// not monopolize the user's CPU or RAM.
type AIBackgroundRemover struct {
	modelPath   string
	runtimePath string

	mu      sync.Mutex
	ready   bool
	initErr error
	session *ort.AdvancedSession
	input   *ort.Tensor[float32]
	output  *ort.Tensor[float32]
}

func NewAIBackgroundRemover(modelPath, runtimePath string) *AIBackgroundRemover {
	r := &AIBackgroundRemover{modelPath: modelPath, runtimePath: runtimePath}
	r.initialize()
	return r
}

func (r *AIBackgroundRemover) Status() (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ready {
		return true, ""
	}
	if r.initErr == nil {
		return false, "AI se još učitava"
	}
	return false, r.initErr.Error()
}

func (r *AIBackgroundRemover) initialize() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ready || r.initErr != nil {
		return
	}
	if _, err := os.Stat(r.modelPath); err != nil {
		r.initErr = fmt.Errorf("AI model nedostaje: %s", r.modelPath)
		return
	}
	if _, err := os.Stat(r.runtimePath); err != nil {
		r.initErr = fmt.Errorf("ONNX Runtime nedostaje: %s", r.runtimePath)
		return
	}
	onnxEnvironmentOnce.Do(func() {
		ort.SetSharedLibraryPath(r.runtimePath)
		onnxEnvironmentErr = ort.InitializeEnvironment()
	})
	if onnxEnvironmentErr != nil {
		r.initErr = fmt.Errorf("nije moguće pokrenuti ONNX Runtime: %w", onnxEnvironmentErr)
		return
	}
	input, err := ort.NewTensor(ort.NewShape(1, 3, u2NetInputSize, u2NetInputSize), make([]float32, 3*u2NetInputSize*u2NetInputSize))
	if err != nil {
		r.initErr = err
		return
	}
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1, u2NetInputSize, u2NetInputSize))
	if err != nil {
		_ = input.Destroy()
		r.initErr = err
		return
	}
	options, err := ort.NewSessionOptions()
	if err != nil {
		_ = input.Destroy()
		_ = output.Destroy()
		r.initErr = err
		return
	}
	defer options.Destroy()
	_ = options.SetIntraOpNumThreads(1)
	_ = options.SetInterOpNumThreads(1)
	session, err := ort.NewAdvancedSession(r.modelPath, []string{"input.1"}, []string{"1959"}, []ort.Value{input}, []ort.Value{output}, options)
	if err != nil {
		_ = input.Destroy()
		_ = output.Destroy()
		r.initErr = fmt.Errorf("nije moguće učitati U²-Net model: %w", err)
		return
	}
	r.input, r.output, r.session, r.ready = input, output, session, true
}

func (r *AIBackgroundRemover) Mask(source image.Image) (*image.RGBA, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ready {
		if r.initErr == nil {
			return nil, fmt.Errorf("AI se još učitava")
		}
		return nil, r.initErr
	}
	resized := resizeRGBABilinear(source, u2NetInputSize, u2NetInputSize)
	input := r.input.GetData()
	for y := 0; y < u2NetInputSize; y++ {
		for x := 0; x < u2NetInputSize; x++ {
			c := resized.RGBAAt(x, y)
			index := y*u2NetInputSize + x
			input[index] = (float32(c.R)/255 - .485) / .229
			input[u2NetInputSize*u2NetInputSize+index] = (float32(c.G)/255 - .456) / .224
			input[2*u2NetInputSize*u2NetInputSize+index] = (float32(c.B)/255 - .406) / .225
		}
	}
	if err := r.session.Run(); err != nil {
		return nil, fmt.Errorf("AI izdvajanje pozadine nije uspjelo: %w", err)
	}
	values := r.output.GetData()
	minimum, maximum := values[0], values[0]
	for _, value := range values {
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	if maximum-minimum < .00001 {
		return nil, fmt.Errorf("AI nije prepoznao proizvod na slici")
	}
	mask := image.NewRGBA(image.Rect(0, 0, u2NetInputSize, u2NetInputSize))
	for y := 0; y < u2NetInputSize; y++ {
		for x := 0; x < u2NetInputSize; x++ {
			alpha := uint8(((values[y*u2NetInputSize+x]-minimum)/(maximum-minimum))*255 + .5)
			mask.SetRGBA(x, y, color.RGBA{255, 255, 255, alpha})
		}
	}
	b := source.Bounds()
	return resizeRGBABilinear(mask, b.Dx(), b.Dy()), nil
}

func removeWithAI(source image.Image, remover *AIBackgroundRemover, detailed *DetailedAIBackgroundRemover, onModel func(string)) (*image.RGBA, *image.RGBA, image.Rectangle, string, error) {
	mask, err := remover.Mask(source)
	if err != nil {
		return nil, nil, image.Rectangle{}, "", err
	}
	if needsDetailedSegmentation(source, mask) {
		if onModel != nil {
			onModel("detailed")
		}
		detailedMask, detailedErr := detailed.Mask(source)
		if detailedErr != nil {
			return nil, nil, image.Rectangle{}, "", detailedErr
		}
		product, finalMask, bounds, extractErr := removeWithMask(source, detailedMask, true)
		return product, finalMask, bounds, "detailed", extractErr
	}
	if onModel != nil {
		onModel("fast")
	}
	product, finalMask, bounds, extractErr := removeWithMask(source, mask, false)
	return product, finalMask, bounds, "fast", extractErr
}

func removeWithMask(source image.Image, mask *image.RGBA, preserveAssembly bool) (*image.RGBA, *image.RGBA, image.Rectangle, error) {
	b := source.Bounds()
	// U²-Net can occasionally preserve a thin, wide studio/floor detail from
	// the supplier photo. It has no place in a cut-out product image and would
	// otherwise appear as a coloured line across the new background.
	if !preserveAssembly {
		removeThinHorizontalArtifacts(mask, b.Dx(), b.Dy())
	}
	// U²-Net occasionally marks a thin floor/desk line as salient. Keep only
	// the largest connected object before calculating the crop; this makes the
	// product fill the canvas instead of shrinking around that stray line.
	background := make([]bool, b.Dx()*b.Dy())
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			// Ignore weak bridges in the AI mask. They often connect a product to
			// a photographed floor line, which then survives as a coloured stripe.
			threshold := uint8(150)
			if preserveAssembly {
				threshold = 18
			}
			background[y*b.Dx()+x] = mask.RGBAAt(x, y).A < threshold
		}
	}
	mainObject := largestForegroundComponent(background, b.Dx(), b.Dy())
	if preserveAssembly {
		// Detailed mode deliberately preserves every foreground region returned
		// by BiRefNet: vehicle + roof rack, wheel spokes and detached details.
		for index := range mainObject {
			mainObject[index] = !background[index]
		}
	}
	minY, maxY := 0, b.Dy()
	if !preserveAssembly {
		minY, maxY = primaryMaskRowRange(mask, mainObject, b.Dx(), b.Dy())
	}
	product := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	box := image.Rect(b.Dx(), b.Dy(), 0, 0)
	solidBox := image.Rect(b.Dx(), b.Dy(), 0, 0)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if !mainObject[y*b.Dx()+x] || y < minY || y >= maxY {
				mask.SetRGBA(x, y, color.RGBA{})
				continue
			}
			alpha := mask.RGBAAt(x, y).A
			if alpha < 18 {
				continue
			}
			c := color.RGBAModel.Convert(source.At(b.Min.X+x, b.Min.Y+y)).(color.RGBA)
			c.A = alpha
			product.SetRGBA(x, y, c)
			box = expandBox(box, x, y)
			// Crop against the confident solid silhouette. The loose alpha fringe
			// is preserved for smooth edges, but must not shrink the visible
			// product by adding a large transparent border around it.
			if alpha >= 210 {
				solidBox = expandBox(solidBox, x, y)
			}
		}
	}
	if !preserveAssembly && solidBox.Dx() >= 8 && solidBox.Dy() >= 8 {
		const cropMargin = 10
		box = solidBox.Inset(-cropMargin).Intersect(image.Rect(0, 0, b.Dx(), b.Dy()))
	}
	if box.Dx() < 8 || box.Dy() < 8 {
		return nil, nil, image.Rectangle{}, fmt.Errorf("AI nije pronašao dovoljno jasan proizvod")
	}
	return product, mask, box, nil
}

func expandBox(box image.Rectangle, x, y int) image.Rectangle {
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
	return box
}

// removeThinHorizontalArtifacts removes isolated, very wide mask bands. This
// deliberately happens before the connected-component pass: a weak AI bridge
// can otherwise make a floor line part of the product component.
func removeThinHorizontalArtifacts(mask *image.RGBA, width, height int) {
	const (
		alphaThreshold = 32
		maxBandHeight  = 14
	)
	minimumWidth := width / 6
	if minimumWidth < 48 {
		minimumWidth = 48
	}
	counts := make([]int, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if mask.RGBAAt(x, y).A >= alphaThreshold {
				counts[y]++
			}
		}
	}
	for y := 0; y < height; {
		for y < height && counts[y] == 0 {
			y++
		}
		start, end, widest := y, y, 0
		for end < height && counts[end] > 0 {
			if counts[end] > widest {
				widest = counts[end]
			}
			end++
		}
		if end > start && end-start <= maxBandHeight && widest >= minimumWidth {
			for clearY := start; clearY < end; clearY++ {
				for x := 0; x < width; x++ {
					mask.SetRGBA(x, clearY, color.RGBA{})
				}
			}
		}
		y = end
	}
}

// The actual product forms a tall, dense band of mask pixels. Reflections or
// label strips near the lower edge are normally only a few rows high, even if
// a faint mask bridge connects them to the product. Keep the strongest row
// group and a small feathering margin around it.
func primaryMaskRowRange(mask *image.RGBA, object []bool, width, height int) (int, int) {
	counts := make([]int, height)
	peak := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if object[y*width+x] && mask.RGBAAt(x, y).A >= 150 {
				counts[y]++
			}
		}
		if counts[y] > peak {
			peak = counts[y]
		}
	}
	// A one-pixel studio/floor artefact can span a lot of the image width.
	// It must not become part of the product simply because a few weak mask
	// pixels connect it. Product rows form a sustained, dense group instead.
	minimumDenseRow := peak / 16
	if minimumDenseRow < 12 {
		minimumDenseRow = 12
	}
	bestStart, bestEnd, bestPixels := 0, height, -1
	for y := 0; y < height; {
		for y < height && counts[y] < minimumDenseRow {
			y++
		}
		start, pixels := y, 0
		for y < height && counts[y] >= minimumDenseRow {
			pixels += counts[y]
			y++
		}
		if y > start && pixels > bestPixels {
			bestStart, bestEnd, bestPixels = start, y, pixels
		}
	}
	if bestPixels <= 0 {
		return 0, height
	}
	const margin = 10
	if bestStart > margin {
		bestStart -= margin
	} else {
		bestStart = 0
	}
	if bestEnd+margin < height {
		bestEnd += margin
	} else {
		bestEnd = height
	}
	return bestStart, bestEnd
}
