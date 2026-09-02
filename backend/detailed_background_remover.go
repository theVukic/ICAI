package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"

	ort "github.com/yalue/onnxruntime_go"
)

// DetailedAIBackgroundRemover creates its BiRefNet session only for a complex
// image, then destroys it immediately after inference to release CPU RAM.
type DetailedAIBackgroundRemover struct {
	modelPath string
}

func NewDetailedAIBackgroundRemover(modelPath string) *DetailedAIBackgroundRemover {
	return &DetailedAIBackgroundRemover{modelPath: modelPath}
}

func (r *DetailedAIBackgroundRemover) Available() error {
	info, err := os.Stat(r.modelPath)
	if err != nil {
		return fmt.Errorf("BiRefNet model nedostaje: %s", r.modelPath)
	}
	if info.Size() < 800<<20 {
		return fmt.Errorf("BiRefNet model je oštećen ili nepotpun")
	}
	return nil
}

func (r *DetailedAIBackgroundRemover) Mask(source image.Image) (*image.RGBA, error) {
	if err := r.Available(); err != nil {
		return nil, err
	}
	inputs, outputs, err := ort.GetInputOutputInfo(r.modelPath)
	if err != nil {
		return nil, fmt.Errorf("nije moguće provjeriti BiRefNet model: %w", err)
	}
	if len(inputs) != 1 || len(outputs) < 1 || len(inputs[0].Dimensions) != 4 || len(outputs[0].Dimensions) != 4 {
		return nil, fmt.Errorf("BiRefNet model ima nepodržan ONNX format")
	}
	inShape, outShape := inputs[0].Dimensions, outputs[0].Dimensions
	inputHeight, inputWidth := int(inShape[2]), int(inShape[3])
	outputHeight, outputWidth := int(outShape[2]), int(outShape[3])
	if inputHeight < 64 || inputWidth < 64 || outputHeight < 1 || outputWidth < 1 {
		return nil, fmt.Errorf("BiRefNet model ima nevažeće dimenzije")
	}
	input, err := ort.NewTensor(ort.NewShape(1, 3, int64(inputHeight), int64(inputWidth)), make([]float32, 3*inputHeight*inputWidth))
	if err != nil {
		return nil, err
	}
	defer input.Destroy()
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1, int64(outputHeight), int64(outputWidth)))
	if err != nil {
		return nil, err
	}
	defer output.Destroy()
	options, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer options.Destroy()
	_ = options.SetIntraOpNumThreads(1)
	_ = options.SetInterOpNumThreads(1)
	_ = options.SetCpuMemArena(false)
	session, err := ort.NewAdvancedSession(r.modelPath, []string{inputs[0].Name}, []string{outputs[0].Name}, []ort.Value{input}, []ort.Value{output}, options)
	if err != nil {
		return nil, fmt.Errorf("nije moguće učitati BiRefNet model: %w", err)
	}
	defer session.Destroy()
	resized := resizeRGBABilinear(source, inputWidth, inputHeight)
	values := input.GetData()
	plane := inputHeight * inputWidth
	for y := 0; y < inputHeight; y++ {
		for x := 0; x < inputWidth; x++ {
			c := resized.RGBAAt(x, y)
			index := y*inputWidth + x
			values[index] = (float32(c.R)/255 - .485) / .229
			values[plane+index] = (float32(c.G)/255 - .456) / .224
			values[plane*2+index] = (float32(c.B)/255 - .406) / .225
		}
	}
	if err := session.Run(); err != nil {
		return nil, fmt.Errorf("detaljno AI izdvajanje pozadine nije uspjelo: %w", err)
	}
	mask := image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	for y := 0; y < outputHeight; y++ {
		for x := 0; x < outputWidth; x++ {
			value := output.GetData()[y*outputWidth+x]
			if value < 0 || value > 1 {
				value = 1 / (1 + float32(math.Exp(float64(-value))))
			}
			alpha := uint8(maxFloat32(0, minFloat32(1, value))*255 + .5)
			mask.SetRGBA(x, y, color.RGBA{255, 255, 255, alpha})
		}
	}
	b := source.Bounds()
	return resizeRGBABilinear(mask, b.Dx(), b.Dy()), nil
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// needsDetailedSegmentation intentionally combines source and fast-mask
// signals. This catches photographed scenes and watermarked catalogue photos
// while retaining U²-Net speed for clean studio images.
func needsDetailedSegmentation(source image.Image, mask *image.RGBA) bool {
	b := source.Bounds()
	if b.Dx() < 2 || b.Dy() < 2 {
		return false
	}
	var borderDeviation, samples, edgeForeground, uncertain, total, nonStudioBorder int
	var meanR, meanG, meanB float64
	smallestSide := b.Dx()
	if b.Dy() < smallestSide {
		smallestSide = b.Dy()
	}
	step := maxInt(1, smallestSide/48)
	for y := 0; y < b.Dy(); y += step {
		for x := 0; x < b.Dx(); x += step {
			alpha := mask.RGBAAt(x, y).A
			total++
			if alpha > 32 && alpha < 224 {
				uncertain++
			}
			if x != 0 && y != 0 && x < b.Dx()-step && y < b.Dy()-step {
				continue
			}
			c := color.RGBAModel.Convert(source.At(b.Min.X+x, b.Min.Y+y)).(color.RGBA)
			meanR += float64(c.R)
			meanG += float64(c.G)
			meanB += float64(c.B)
			samples++
			maximum, minimum := c.R, c.R
			if c.G > maximum {
				maximum = c.G
			}
			if c.B > maximum {
				maximum = c.B
			}
			if c.G < minimum {
				minimum = c.G
			}
			if c.B < minimum {
				minimum = c.B
			}
			if maximum-minimum > 18 || (int(c.R)+int(c.G)+int(c.B))/3 < 220 {
				nonStudioBorder++
			}
			if alpha >= 150 {
				edgeForeground++
			}
		}
	}
	if samples == 0 {
		return false
	}
	meanR /= float64(samples)
	meanG /= float64(samples)
	meanB /= float64(samples)
	for y := 0; y < b.Dy(); y += step {
		for x := 0; x < b.Dx(); x += step {
			if x != 0 && y != 0 && x < b.Dx()-step && y < b.Dy()-step {
				continue
			}
			c := color.RGBAModel.Convert(source.At(b.Min.X+x, b.Min.Y+y)).(color.RGBA)
			borderDeviation += int(math.Abs(float64(c.R)-meanR) + math.Abs(float64(c.G)-meanG) + math.Abs(float64(c.B)-meanB))
		}
	}
	return borderDeviation/samples > 18 || nonStudioBorder > samples/5 || edgeForeground > samples/7 || uncertain > total/9
}
