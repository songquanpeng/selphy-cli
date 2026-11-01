package photo

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type Profile struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Code   uint16 `json:"protocol_code"`
	Family string `json:"family"`
}

var Profiles = []Profile{
	{"postcard", 1248, 1872, 4, "CP"},
	{"l", 1152, 1472, 3, "CP"},
	{"card", 1088, 668, 2, "CP"},
	{"qx-square", 768, 768, 0, "QX"},
	{"qx-square-borderless", 848, 826, 17, "QX20"},
	{"qx-card", 644, 826, 18, "QX20"},
}

func NormalizeModel(s string) (string, error) {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
	s = strings.TrimPrefix(s, "CANON SELPHY ")
	s = strings.TrimPrefix(s, "SELPHY ")
	switch s {
	case "CP1500", "QX10", "QX20":
		return s, nil
	}
	return "", fmt.Errorf("unsupported model %q; this CPNP client supports CP1500, QX10 and QX20", s)
}
func GetProfile(model, paper string) (Profile, error) {
	m, err := NormalizeModel(model)
	if err != nil {
		return Profile{}, err
	}
	if paper == "" || paper == "auto" {
		if strings.HasPrefix(m, "CP") {
			paper = "postcard"
		} else {
			paper = "qx-square"
		}
	}
	for _, p := range Profiles {
		if p.Name == paper {
			if strings.HasPrefix(m, "CP") != (p.Family == "CP") || p.Family == "QX20" && m != "QX20" {
				return Profile{}, fmt.Errorf("paper %s is incompatible with %s", paper, m)
			}
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("unknown paper %q; run selphy papers", paper)
}

type Image struct {
	JPEG            []byte `json:"-"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	Bytes           int    `json:"bytes"`
	SourceWidth     int    `json:"source_width"`
	SourceHeight    int    `json:"source_height"`
	EXIFOrientation int    `json:"exif_orientation"`
	Area            string `json:"area"`
	ContentRect     Rect   `json:"content_rect"`
	PaperRect       Rect   `json:"paper_rect"`
	SafeRect        Rect   `json:"safe_rect"`
}

func Prepare(path string, p Profile, fit string, rotate, quality int) (Image, error) {
	return PrepareWithOptions(path, p, Options{Fit: fit, Rotate: rotate, Quality: quality, Area: "bleed"})
}

type Options struct {
	Fit, Area       string
	Rotate, Quality int
	MarginMM        float64
}

func PrepareWithOptions(path string, p Profile, options Options) (Image, error) {
	fit, rotate, quality := options.Fit, options.Rotate, options.Quality
	content, err := ContentBounds(p, options.Area, options.MarginMM)
	if err != nil {
		return Image{}, err
	}
	if fit != "cover" && fit != "contain" {
		return Image{}, fmt.Errorf("--fit must be cover or contain")
	}
	if rotate%90 != 0 {
		return Image{}, fmt.Errorf("--rotate must be a multiple of 90")
	}
	if quality < 1 || quality > 100 {
		return Image{}, fmt.Errorf("JPEG quality must be 1..100")
	}
	f, err := os.Open(path)
	if err != nil {
		return Image{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Image{}, err
	}
	if st.Size() > 128<<20 {
		return Image{}, fmt.Errorf("input exceeds 128 MiB")
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return Image{}, fmt.Errorf("decode image (JPEG, PNG, WebP supported): %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 100_000_000 {
		return Image{}, fmt.Errorf("image exceeds 100 megapixel limit")
	}
	if _, err = f.Seek(0, 0); err != nil {
		return Image{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Image{}, err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, err
	}
	orientation := exifOrientation(data)
	src = orient(src, orientation)
	for i := 0; i < (rotate%360+360)%360/90; i++ {
		src = orient(src, 6)
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewRGBA(image.Rect(0, 0, p.Width, p.Height))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if fit == "cover" {
		cw, ch := sw, sh
		if int64(sw)*int64(content.Dy()) > int64(sh)*int64(content.Dx()) {
			cw = max(1, int(math.Round(float64(sh)*float64(content.Dx())/float64(content.Dy()))))
		} else {
			ch = max(1, int(math.Round(float64(sw)*float64(content.Dy())/float64(content.Dx()))))
		}
		crop := image.Rect((sw-cw)/2, (sh-ch)/2, (sw+cw)/2, (sh+ch)/2).Add(src.Bounds().Min)
		draw.CatmullRom.Scale(dst, content, src, crop, draw.Over, nil)
	} else {
		scale := math.Min(float64(content.Dx())/float64(sw), float64(content.Dy())/float64(sh))
		w, h := max(1, int(math.Round(float64(sw)*scale))), max(1, int(math.Round(float64(sh)*scale)))
		x, y := content.Min.X+(content.Dx()-w)/2, content.Min.Y+(content.Dy()-h)/2
		draw.CatmullRom.Scale(dst, image.Rect(x, y, x+w, y+h), src, src.Bounds(), draw.Over, nil)
	}
	var out bytes.Buffer
	if err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: quality}); err != nil {
		return Image{}, err
	}
	return Image{JPEG: out.Bytes(), Width: p.Width, Height: p.Height, Bytes: out.Len(), SourceWidth: sw, SourceHeight: sh, EXIFOrientation: orientation, Area: options.Area, ContentRect: rect(content), PaperRect: rect(PaperBounds(p)), SafeRect: rect(SafeBounds(p))}, nil
}

func orient(src image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			sx, sy := x, y
			switch o {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			dst.Set(x, y, src.At(src.Bounds().Min.X+sx, src.Bounds().Min.Y+sy))
		}
	}
	return dst
}
