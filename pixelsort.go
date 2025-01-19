package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/image/tiff"
)

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
					mask.Set(x, y, color.Black)
				} else {
					mask.Set(x, y, color.White)
				}
			} else {
				if !invert {
					mask.Set(x, y, color.White)
				} else {
					mask.Set(x, y, color.Black)
				}
			}
		}
	}

	return mask, nil
}

type Span struct {
	color color.Color
	row   int
	idx   int
	len   int
}

func generateSortSpans(mask image.Image, minSpanLen int) []Span {
	var spans []Span = make([]Span, 0)
	var keep bool = mask.At(mask.Bounds().Min.X, mask.Bounds().Min.Y) == color.RGBA{255, 255, 255, 255}
	span := Span{mask.At(mask.Bounds().Min.X, mask.Bounds().Min.Y), mask.Bounds().Min.Y, mask.Bounds().Min.X, 1}

	for y := range mask.Bounds().Max.Y {
		for x := range mask.Bounds().Max.X {
			if mask.At(x, y) == span.color {
				span.len++
			} else {
				if keep && span.len >= minSpanLen {
					spans = append(spans, span)
				}
				span = Span{mask.At(x, y), y, x, 1}
				keep = !keep
			}
		}
	}

	return spans
}

func debugColorSpans(mask image.Image, spans []Span) image.Image {
	img := image.NewRGBA(mask.Bounds())

	for _, span := range spans {
		for i := span.idx; i < span.idx+span.len; i++ {
			img.Set(i, span.row, color.RGBA{255, 0, 255, 255})
		}
	}

	return img
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

func sortSpans(src image.Image, spans []Span) image.Image {
	out := image.NewRGBA(src.Bounds())
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)

	for _, span := range spans {
		c := make([]color.Color, span.len)
		for i := range span.len {
			c[i] = src.At(span.idx+i, span.row)
		}

		sort.Slice(c, func(i, j int) bool {
			a := getHue(c[i])
			b := getHue(c[j])
			return a > b
		})

		for i := range span.len {
			out.Set(span.idx+i, span.row, c[i])
		}
	}

	return out
}

func main() {
	var filepath string
	lo := lowThreshold
	hi := highThreshold
	inverted := false
	if len(os.Args) < 2 {
		fmt.Println("Usage: <filepath>")
		os.Exit(1)
	}
	if len(os.Args) > 2 && len(os.Args) != 5 {
		fmt.Println("Usage: <filepath> <low threshold> <high threshold> <inverted>")
		os.Exit(1)
	} else if len(os.Args) == 2 {
		filepath = os.Args[1]
	} else if len(os.Args) == 5 {
		filepath = os.Args[1]
		i1, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Println("Low threshold should be a whole number of 0 or greater.")
			os.Exit(1)
		}
		lo = i1
		i2, err := strconv.Atoi(os.Args[3])
		if err != nil {
			fmt.Println("High threshold should be a whole number of 0 or greater.")
			os.Exit(1)
		}
		hi = i2
		i3 := strings.ToLower(os.Args[4])
		switch i3 {
		case "y", "yes", "t", "true":
			inverted = true
		case "n", "no", "f", "false":
			inverted = false
		default:
			fmt.Println("Use the format y/n to specify an inversion of the threshold mask.")
			os.Exit(1)
		}
	}

	var preserveFormat bool = false

	img, format, err := decodeImage(filepath)
	if err != nil {
		panic(err.Error())
	}

	mask, err := generateLuminanceMask(img, lo, hi, inverted)
	if err != nil {
		panic(err.Error())
	}
	spans := generateSortSpans(mask, 2)
	out := sortSpans(img, spans)

	if !preserveFormat {
		format = "png"
	}
	err = encodeImage(fmt.Sprintf("./output/mask.%s", format), mask, format)
	if err != nil {
		panic(err.Error())
	}
	err = encodeImage(fmt.Sprintf("./output/out.%s", format), out, format)
	if err != nil {
		panic(err.Error())
	}
}
