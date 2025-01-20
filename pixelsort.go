package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"sort"

	"rsc.io/getopt"

	"golang.org/x/image/tiff"
)

// https://reintech.io/blog/a-guide-to-gos-image-package-manipulating-and-processing-images
func decodeImage(filename string) (image.Image, string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	img, format, err := image.Decode(file)
	if err != nil {
		return nil, "", err
	}

	return img, format, nil
}

// https://reintech.io/blog/a-guide-to-gos-image-package-manipulating-and-processing-images
func encodeImage(filename string, img image.Image, format string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	switch format {
	case "jpeg", "jpg":
		return jpeg.Encode(file, img, nil)
	case "png":
		return png.Encode(file, img)
	case "tiff":
		return tiff.Encode(file, img, nil)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

const lowThreshold int = 10000
const highThreshold int = 30000

// https://www.itu.int/rec/R-REC-BT.601
const perceivedR float64 = 0.299
const perceivedG float64 = 0.587
const perceivedB float64 = 0.114

var RGBAWhite color.RGBA = color.RGBA{255, 255, 255, 255}
var RGBABlack color.RGBA = color.RGBA{0, 0, 0, 255}
var RGBAGreen color.RGBA = color.RGBA{0, 255, 0, 255}
var RGBAMagenta color.RGBA = color.RGBA{255, 0, 255, 255}

func generateLuminanceMask(original image.Image, lo int, hi int, invert bool) (image.Image, error) {
	if lo > hi {
		return nil, errors.New("Low threshold must be less than high threshold.")
	}
	if lo < 0 || hi < 0 {
		return nil, errors.New("Threshold values must be positive.")
	}

	mask := image.NewRGBA(original.Bounds())

	for y := range original.Bounds().Max.Y {
		for x := range original.Bounds().Max.X {
			r, g, b, _ := original.At(x, y).RGBA()
			perceivedLuminance := math.Sqrt(perceivedR*math.Pow(float64(r), 2) + perceivedG*math.Pow(float64(g), 2) + perceivedB*math.Pow(float64(b), 2))
			if perceivedLuminance < float64(lo) || perceivedLuminance > float64(hi) {
				if !invert {
					mask.Set(x, y, RGBABlack)
				} else {
					mask.Set(x, y, RGBAWhite)
				}
			} else {
				if !invert {
					mask.Set(x, y, RGBAWhite)
				} else {
					mask.Set(x, y, RGBABlack)
				}
			}
		}
	}

	return mask, nil
}

type Span struct {
	id  int
	idx int
	len int
}

type ColorSpan struct {
	pixels []color.Color
	id     int
	idx    int
}

type SpanType int

const (
	Horizontal SpanType = iota
	Vertical
	Diagonal
)

func generateHorizontalSpans(mask image.Image, minSpanLen int) []Span {
	var spans []Span = make([]Span, 0)

	for y := range mask.Bounds().Dy() {
		var currentColor = mask.At(0, y)
		var keep bool = currentColor == RGBAWhite
		var span Span = Span{y, 0, 0}

		for x := range mask.Bounds().Dx() {
			if mask.At(x, y) == currentColor {
				span.len++
			} else {
				if keep && span.len >= minSpanLen {
					spans = append(spans, span)
				}
				currentColor = mask.At(x, y)
				span = Span{y, x, 0}
				keep = !keep
			}

			if x == mask.Bounds().Dx()-1 && keep {
				spans = append(spans, span)
			}
		}
	}

	return spans
}

func generateVerticalSpans(mask image.Image, minSpanLen int) []Span {
	var spans []Span = make([]Span, 0)

	for x := range mask.Bounds().Dx() {
		var currentColor = mask.At(x, 0)
		var keep bool = currentColor == RGBAWhite
		var span Span = Span{x, 0, 0}

		for y := range mask.Bounds().Dy() {
			if mask.At(x, y) == currentColor {
				span.len++
			} else {
				if keep && span.len >= minSpanLen {
					spans = append(spans, span)
				}
				currentColor = mask.At(x, y)
				span = Span{x, y, 0}
				keep = !keep
			}

			if y == mask.Bounds().Dy()-1 && keep {
				spans = append(spans, span)
			}
		}
	}

	return spans
}

func debugHorizontalSpans(mask image.Image, spans []Span) {
	b := mask.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))

	for _, span := range spans {
		for i := range span.len {
			img.Set(span.idx+i, span.id, RGBAGreen)
		}
	}

	err := encodeImage(fmt.Sprintf("./output/spanDBG.png"), img, "png")
	if err != nil {
		panic(err.Error())
	}
}

func debugVerticalSpans(mask image.Image, spans []Span) {
	b := mask.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))

	for _, span := range spans {
		for i := range span.len {
			img.Set(span.id, span.idx+i, RGBAGreen)
		}
	}

	err := encodeImage(fmt.Sprintf("./output/spanDBG.png"), img, "png")
	if err != nil {
		panic(err.Error())
	}
}

// https://stackoverflow.com/questions/23090019/fastest-formula-to-get-hue-from-rgb
func getHue(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	red := float64(r)
	green := float64(g)
	blue := float64(b)
	var min float64 = math.Min(math.Min(red, green), blue)
	var max float64 = math.Max(math.Max(red, green), blue)

	if min == max {
		return 0
	}

	var hue float64
	if max == red {
		hue = (green - blue) / (max - min)
	} else if max == green {
		hue = 2 + (blue-red)/(max-min)
	} else {
		hue = 4 + (red-green)/(max-min)
	}

	hue = hue * 60
	if hue < 0 {
		hue = hue + 360
	}

	return math.Round(hue)
}

func generateHorizontalColorSpans(img image.Image, spans []Span) []ColorSpan {
	var cspans []ColorSpan = make([]ColorSpan, len(spans))

	for _, span := range spans {
		c := make([]color.Color, span.len)
		for i := range span.len {
			c[i] = img.At(span.idx+i, span.id)
		}
		cspans = append(cspans, ColorSpan{c, span.id, span.idx})
	}

	return cspans
}

func generateVerticalColorSpans(img image.Image, spans []Span) []ColorSpan {
	var cspans []ColorSpan = make([]ColorSpan, len(spans))

	for _, span := range spans {
		c := make([]color.Color, span.len)
		for i := range span.len {
			c[i] = img.At(span.id, span.idx+i)
		}
		cspans = append(cspans, ColorSpan{c, span.id, span.idx})
	}

	return cspans
}

func sortSpans(spans []ColorSpan, reverse bool) []ColorSpan {
	var sortedSpans []ColorSpan = make([]ColorSpan, 0)
	for _, span := range spans {
		if len(span.pixels) > 1 {
			sort.Slice(span.pixels, func(i, j int) bool {
				a := getHue(span.pixels[i])
				b := getHue(span.pixels[j])
				if !reverse {
					return a > b
				} else {
					return a < b
				}
			})
			sortedSpans = append(sortedSpans, span)
		}
	}

	return sortedSpans
}

func applyHorizontalSpans(src image.Image, spans []ColorSpan) image.Image {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)

	for _, span := range spans {
		for i, c := range span.pixels {
			out.Set(span.idx+i, span.id, c)
		}
	}

	return out
}

func applyVerticalSpans(src image.Image, spans []ColorSpan) image.Image {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)

	for _, span := range spans {
		for i, c := range span.pixels {
			out.Set(span.id, span.idx+i, c)
		}
	}

	return out
}

func main() {
	flag.Usage = func() {
		w := flag.CommandLine.Output()

		fmt.Fprintf(w, "Usage: [options] <filename>\nOptions:\n")
		getopt.PrintDefaults()
	}

	lowerthreshold := flag.Int("l", lowThreshold, "Lower perceived luminance threshold when generating a mask for the image.")
	upperthreshold := flag.Int("u", highThreshold, "Upper perceived luminance threshold when generating a mask for the image.")
	minspanlength := flag.Int("s", 2, "The minimum allowed length of span that should be sorted.")
	spantype := flag.Int("t", 0, "The type of sorting to do, 0: horizontal, 1: vertical, 2: diagonal.")
	keepmask := flag.Bool("m", false, "Produce an output file for the generated mask.")
	inverted := flag.Bool("i", false, "Invert the mask for sortable image areas.")
	reverse := flag.Bool("r", false, "Reverse the sorting direction.")
	preserveformat := flag.Bool("p", false, "Produce output in the same image format of the provided input.")

	getopt.Aliases(
		"l", "lower-threshold",
		"u", "upper-threshold",
		"s", "minimum-span-length",
		"t", "span-type",
		"m", "keep-mask",
		"i", "invert",
		"r", "reverse",
		"p", "preserve-format",
	)

	getopt.Parse()
	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(0)
	}
	filepath := flag.Args()[0]

	img, format, err := decodeImage(filepath)
	if err != nil {
		panic(err.Error())
	}

	mask, err := generateLuminanceMask(img, *lowerthreshold, *upperthreshold, *inverted)
	if err != nil {
		panic(err.Error())
	}

	var spans []Span
	var cspans []ColorSpan
	var out image.Image
	switch SpanType(*spantype) {
	case Horizontal:
		spans = generateHorizontalSpans(mask, *minspanlength)
		cspans = generateHorizontalColorSpans(img, spans)
		cspans = sortSpans(cspans, *reverse)
		out = applyHorizontalSpans(img, cspans)
	case Vertical:
		spans = generateVerticalSpans(mask, *minspanlength)
		cspans = generateVerticalColorSpans(img, spans)
		cspans = sortSpans(cspans, *reverse)
		out = applyVerticalSpans(img, cspans)
	default:
		fmt.Println("Unimplemented sorting type.")
		os.Exit(0)
	}

	if !*preserveformat {
		format = "png"
	}
	err = encodeImage(fmt.Sprintf("./output/out.%s", format), out, format)
	if err != nil {
		panic(err.Error())
	}
	if *keepmask {
		err = encodeImage(fmt.Sprintf("./output/mask.%s", format), mask, format)
		if err != nil {
			panic(err.Error())
		}
	}
}
