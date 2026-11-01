package photo

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestTrimPreviewsKeepPrintRasterUnchanged(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 200; x++ {
			src.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y % 240), 100, 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	im := Image{JPEG: encoded.Bytes(), Width: 200, Height: 300, PaperRect: Rect{20, 30, 160, 240}, SafeRect: Rect{30, 40, 140, 220}, Area: "bleed", Fit: "cover", TrimBasis: "saved-calibration"}
	before := bytes.Clone(im.JPEG)
	guide, err := TrimGuide(im)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := png.Decode(bytes.NewReader(guide))
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Bounds().Dx() != 800 || rendered.Bounds().Dy() != 516 {
		t.Fatal(rendered.Bounds())
	}
	assertColor := func(x, y int, want color.RGBA) {
		t.Helper()
		got := color.RGBAModel.Convert(rendered.At(x, y)).(color.RGBA)
		if got != want {
			t.Fatalf("pixel %d,%d: got %v want %v", x, y, got, want)
		}
	}
	// 300 px horizontal centering, 112 px header. Guide colors must land on
	// physical trim/safe coordinates, not source-photo or content offsets.
	assertColor(320, 162, color.RGBA{225, 38, 51, 255})
	assertColor(330, 160, color.RGBA{0, 150, 95, 255})
	decoded, _ := jpeg.Decode(bytes.NewReader(im.JPEG))
	r, g, b, _ := decoded.At(5, 5).RGBA()
	assertColor(305, 117, color.RGBA{uint8((r >> 8) * 2 / 5), uint8((g >> 8) * 2 / 5), uint8((b >> 8) * 2 / 5), 255})
	trimmed, err := TrimmedPreview(im)
	if err != nil {
		t.Fatal(err)
	}
	finished, err := jpeg.Decode(bytes.NewReader(trimmed))
	if err != nil {
		t.Fatal(err)
	}
	if finished.Bounds().Dx() != 160 || finished.Bounds().Dy() != 240 {
		t.Fatal(finished.Bounds())
	}
	// The retained center must come from the predicted sheet's center.
	want := color.RGBAModel.Convert(decoded.At(100, 150)).(color.RGBA)
	got := color.RGBAModel.Convert(finished.At(80, 120)).(color.RGBA)
	for _, v := range [][2]uint8{{got.R, want.R}, {got.G, want.G}, {got.B, want.B}} {
		d := int(v[0]) - int(v[1])
		if d < -4 || d > 4 {
			t.Fatalf("finished preview sampled wrong region: %v vs %v", got, want)
		}
	}
	if !bytes.Equal(before, im.JPEG) {
		t.Fatal("view-only previews altered the print raster")
	}
}
