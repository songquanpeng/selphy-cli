package photo

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareCropContainAndRotation(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 120, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 120; x++ {
			src.Set(x, y, color.NRGBA{R: 240, A: 255})
		}
	}
	path := filepath.Join(t.TempDir(), "input.png")
	f, _ := os.Create(path)
	if err := png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
	f.Close()
	p := Profile{Width: 100, Height: 100}
	for _, fit := range []string{"cover", "contain"} {
		t.Run(fit, func(t *testing.T) {
			result, err := Prepare(path, p, fit, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			out, err := jpeg.Decode(bytes.NewReader(result.JPEG))
			if err != nil {
				t.Fatal(err)
			}
			if out.Bounds().Dx() != 100 || out.Bounds().Dy() != 100 {
				t.Fatal(out.Bounds())
			}
			if fit == "cover" && result.SourceCrop != (Rect{30, 0, 60, 60}) {
				t.Fatalf("incorrect source crop: %+v", result.SourceCrop)
			}
			if fit == "contain" && result.SourceCrop != (Rect{0, 0, 120, 60}) {
				t.Fatalf("contain cropped the source: %+v", result.SourceCrop)
			}
			r, g, b, _ := out.At(50, 1).RGBA()
			if fit == "contain" && min(r, g, b) < 60000 {
				t.Fatal("contain lost white margin")
			}
			if fit == "cover" && (r < 50000 || g > 2000 || b > 2000) {
				t.Fatal("cover added a margin or cropped incorrectly")
			}
		})
	}
	rotated, err := Prepare(path, p, "contain", 90, 95)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.SourceWidth != 60 || rotated.SourceHeight != 120 {
		t.Fatal("rotation dimensions incorrect")
	}
	if _, err = Prepare(path, p, "stretch", 0, 95); err == nil {
		t.Fatal("invalid fit accepted")
	}
	if _, err = Prepare(path, p, "cover", 1, 95); err == nil {
		t.Fatal("invalid rotation accepted")
	}
}

func TestEXIFOrientations(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(1 + y*3 + x), A: 255})
		}
	}
	want := map[int][]uint8{1: {1, 2, 3, 4, 5, 6}, 2: {3, 2, 1, 6, 5, 4}, 3: {6, 5, 4, 3, 2, 1}, 4: {4, 5, 6, 1, 2, 3}, 5: {1, 4, 2, 5, 3, 6}, 6: {4, 1, 5, 2, 6, 3}, 7: {6, 3, 5, 2, 4, 1}, 8: {3, 6, 2, 5, 1, 4}}
	for o, expected := range want {
		got := orient(src, o)
		i := 0
		for y := 0; y < got.Bounds().Dy(); y++ {
			for x := 0; x < got.Bounds().Dx(); x++ {
				r, _, _, _ := got.At(x, y).RGBA()
				if uint8(r>>8) != expected[i] {
					t.Fatalf("orientation %d pixel %d: got %d want %d", o, i, r>>8, expected[i])
				}
				i++
			}
		}
	}
	for _, big := range []bool{true, false} {
		var order binary.ByteOrder = binary.LittleEndian
		header := "II"
		if big {
			order = binary.BigEndian
			header = "MM"
		}
		b := make([]byte, 26)
		copy(b, header)
		order.PutUint16(b[2:], 42)
		order.PutUint32(b[4:], 8)
		order.PutUint16(b[8:], 1)
		order.PutUint16(b[10:], 0x112)
		order.PutUint16(b[12:], 3)
		order.PutUint32(b[14:], 1)
		order.PutUint16(b[18:], 6)
		if got := tiffOrientation(b); got != 6 {
			t.Fatalf("big=%t: got %d", big, got)
		}
		for n := 0; n < 22; n++ {
			if got := tiffOrientation(b[:n]); got != 1 {
				t.Fatalf("truncated TIFF length %d returned %d", n, got)
			}
		}
		app := append([]byte("Exif\x00\x00"), b...)
		jpg := []byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(app) + 2)}
		jpg = append(jpg, app...)
		if got := exifOrientation(jpg); got != 6 {
			t.Fatalf("JPEG EXIF got %d", got)
		}
	}
}
func TestProfileCompatibility(t *testing.T) {
	if p, err := GetProfile("Canon SELPHY CP1500", "postcard"); err != nil || p.Width != 1248 || p.Height != 1872 {
		t.Fatalf("%+v %v", p, err)
	}
	for _, tc := range [][2]string{{"CP1500", "qx-card"}, {"QX10", "qx-square-borderless"}, {"CP1300", "postcard"}} {
		if _, err := GetProfile(tc[0], tc[1]); err == nil {
			t.Fatalf("accepted incompatible profile %v", tc)
		}
	}
}

func TestSafeLayoutProtectsContentFromTrim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layout.png")
	src := image.NewNRGBA(image.Rect(0, 0, 100, 148))
	for y := 0; y < 148; y++ {
		for x := 0; x < 100; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 230, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = png.Encode(f, src)
	f.Close()
	p, _ := GetProfile("CP1500", "postcard")
	im, err := PrepareWithOptions(path, p, Options{Fit: "contain", Area: "safe", Quality: 100})
	if err != nil {
		t.Fatal(err)
	}
	if im.ContentRect != (Rect{55, 84, 1138, 1704}) || im.PaperRect != (Rect{32, 60, 1184, 1752}) {
		t.Fatalf("incorrect geometry: %+v", im)
	}
	out, err := jpeg.Decode(bytes.NewReader(im.JPEG))
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range []image.Point{{32, 60}, {47, 500}, {1201, 500}, {600, 80}, {600, 1792}} {
		r, g, b, _ := out.At(pt.X, pt.Y).RGBA()
		if min(r, g, b) < 60000 {
			t.Fatalf("artwork reached outside the safe area at %v", pt)
		}
	}
	r, g, b, _ := out.At(624, 936).RGBA()
	if r < 50000 || g > 1000 || b > 1000 {
		t.Fatal("artwork center lost")
	}
	withMargin, err := ContentBounds(p, "paper", 3)
	if err != nil || withMargin != (image.Rect(68, 96, 1180, 1776)) {
		t.Fatalf("margin: %v %v", withMargin, err)
	}
	for _, margin := range []float64{-1, 100} {
		if _, err := ContentBounds(p, "safe", margin); err == nil {
			t.Fatalf("invalid margin %g accepted", margin)
		}
	}
}

func TestCalibrationRejectsInvalidOffsets(t *testing.T) {
	p, _ := GetProfile("CP1500", "postcard")
	if _, err := PrepareWithOptions("unused", p, Options{Fit: "contain", Area: "safe", Quality: 95, OffsetXMM: -10}); err == nil {
		t.Fatal("clipped safe content accepted")
	}
	if _, err := OffsetPixels(11, 0); err == nil {
		t.Fatal("excessive offset accepted")
	}
	point, err := OffsetPixels(-.8, -.5)
	if err != nil || point != image.Pt(-9, -6) {
		t.Fatalf("offset conversion: %v %v", point, err)
	}
}
