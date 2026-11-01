package photo

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// TrimGuide produces a separate, annotated PNG. It never modifies the JPEG
// that is sent to the printer. All geometry is relative to that original JPEG.
func TrimGuide(im Image) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(im.JPEG))
	if err != nil {
		return nil, err
	}
	const header, footer = 112, 104
	w := max(im.Width, 800)
	dst := image.NewRGBA(image.Rect(0, 0, w, header+im.Height+footer))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.RGBA{246, 248, 251, 255}), image.Point{}, draw.Src)
	origin := image.Pt((w-im.Width)/2, header)
	frame := image.Rectangle{Min: origin, Max: origin.Add(image.Pt(im.Width, im.Height))}
	draw.Draw(dst, frame, src, src.Bounds().Min, draw.Src)
	trim, safe := im.PaperRect.Rectangle(), im.SafeRect.Rectangle()
	// Darken only material expected to be lost after trimming. The print raster
	// itself remains byte-for-byte unchanged.
	for y := 0; y < im.Height; y++ {
		for x := 0; x < im.Width; x++ {
			if !image.Pt(x, y).In(trim) {
				pt := origin.Add(image.Pt(x, y))
				r, g, b, _ := dst.At(pt.X, pt.Y).RGBA()
				dst.SetRGBA(pt.X, pt.Y, color.RGBA{uint8((r >> 8) * 2 / 5), uint8((g >> 8) * 2 / 5), uint8((b >> 8) * 2 / 5), 255})
			}
		}
	}
	stroke := func(r image.Rectangle, c color.Color, dashed bool) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for _, x := range []int{r.Min.X, r.Min.X + 1, r.Min.X + 2, r.Max.X - 3, r.Max.X - 2, r.Max.X - 1} {
				pt := image.Pt(x, y)
				if pt.In(src.Bounds()) && (!dashed || y/12%2 == 0) {
					dst.Set(pt.X+origin.X, pt.Y+origin.Y, c)
				}
			}
		}
		for x := r.Min.X; x < r.Max.X; x++ {
			for _, y := range []int{r.Min.Y, r.Min.Y + 1, r.Min.Y + 2, r.Max.Y - 3, r.Max.Y - 2, r.Max.Y - 1} {
				pt := image.Pt(x, y)
				if pt.In(src.Bounds()) && (!dashed || x/12%2 == 0) {
					dst.Set(pt.X+origin.X, pt.Y+origin.Y, c)
				}
			}
		}
	}
	red, green := color.RGBA{225, 38, 51, 255}, color.RGBA{0, 150, 95, 255}
	stroke(trim, red, false)
	stroke(safe, green, true)
	label := func(x, y, scale int, s string, c color.Color) {
		tmp := image.NewRGBA(image.Rect(0, 0, len(s)*7+4, 18))
		d := font.Drawer{Dst: tmp, Src: image.NewUniform(c), Face: basicfont.Face7x13, Dot: fixed.P(2, 13)}
		d.DrawString(s)
		draw.NearestNeighbor.Scale(dst, image.Rect(x, y, x+tmp.Bounds().Dx()*scale, y+18*scale), tmp, tmp.Bounds(), draw.Over, nil)
	}
	ink := color.RGBA{25, 36, 51, 255}
	label(20, 12, 2, "SELPHY / TRIM PREVIEW", ink)
	basis := "MODEL ESTIMATE - NOT PHYSICALLY CALIBRATED"
	if im.TrimBasis == "saved-calibration" {
		basis = "SAVED PRINTER/PAPER CALIBRATION - ESTIMATE"
	}
	label(20, 55, 1, basis, ink)
	label(20, 80, 1, "Guide only. The lines and shading are NOT sent to the printer.", ink)
	bottom := header + im.Height
	label(20, bottom+10, 1, "RED: expected finished edge | GREEN DASHES: safe area | SHADED: trimmed away", ink)
	label(20, bottom+34, 1, fmt.Sprintf("Trim x=%d y=%d %dx%d px | Layout %s / %s", trim.Min.X, trim.Min.Y, trim.Dx(), trim.Dy(), im.Area, im.Fit), ink)
	label(20, bottom+58, 1, "Feed varies per sheet. Keep text and important borders inside the green area.", ink)
	label(20, bottom+81, 1, "Use --preview-trimmed for an unmarked finished-sheet estimate.", ink)
	var out bytes.Buffer
	if err = png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// TrimmedPreview simulates the retained sheet. It is for viewing, not a new
// print-ready raster: printing it again would apply another layout/trim pass.
func TrimmedPreview(im Image) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(im.JPEG))
	if err != nil {
		return nil, err
	}
	trim := im.PaperRect.Rectangle()
	if trim.Empty() || trim.Dx() > 10000 || trim.Dy() > 10000 {
		return nil, fmt.Errorf("invalid finished-sheet bounds")
	}
	dst := image.NewRGBA(image.Rect(0, 0, trim.Dx(), trim.Dy()))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, trim.Min, draw.Src)
	var out bytes.Buffer
	if err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 100}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
