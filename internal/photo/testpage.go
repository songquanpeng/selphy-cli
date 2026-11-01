package photo

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// TestPage maps the RP-108 finished sheet and APK safe area into the CPNP raster.
// Rulers are nominal; physical feed/cut tolerances must be checked on the print.
func TestPage() image.Image {
	p := Profiles[0]
	im := image.NewRGBA(image.Rect(0, 0, p.Width, p.Height))
	paper, safe := PaperBounds(p), SafeBounds(p)
	ink := color.RGBA{25, 36, 51, 255}
	red := color.RGBA{199, 40, 46, 255}
	orange := color.RGBA{217, 119, 6, 255}
	green := color.RGBA{0, 123, 87, 255}
	gray := color.RGBA{132, 144, 158, 255}
	fill := func(r image.Rectangle, c color.Color) { draw.Draw(im, r, image.NewUniform(c), image.Point{}, draw.Src) }
	line := func(x1, y1, x2, y2, width int, c color.Color) {
		if x1 == x2 {
			fill(image.Rect(x1-width/2, min(y1, y2), x1+(width+1)/2, max(y1, y2)+1), c)
		} else {
			fill(image.Rect(min(x1, x2), y1-width/2, max(x1, x2)+1, y1+(width+1)/2), c)
		}
	}
	frame := func(r image.Rectangle, width int, c color.Color) {
		fill(image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+width), c)
		fill(image.Rect(r.Min.X, r.Max.Y-width, r.Max.X, r.Max.Y), c)
		fill(image.Rect(r.Min.X, r.Min.Y, r.Min.X+width, r.Max.Y), c)
		fill(image.Rect(r.Max.X-width, r.Min.Y, r.Max.X, r.Max.Y), c)
	}
	label := func(x, y, scale int, text string, c color.Color) {
		tmp := image.NewRGBA(image.Rect(0, 0, len(text)*7+4, 18))
		d := font.Drawer{Dst: tmp, Src: image.NewUniform(c), Face: basicfont.Face7x13, Dot: fixed.P(2, 13)}
		d.DrawString(text)
		draw.NearestNeighbor.Scale(im, image.Rect(x, y, x+tmp.Bounds().Dx()*scale, y+18*scale), tmp, tmp.Bounds(), draw.Over, nil)
	}
	center := func(y, scale int, text string, c color.Color) {
		label((p.Width-(len(text)*7+4)*scale)/2, y, scale, text, c)
	}
	fill(im.Bounds(), color.RGBA{255, 217, 217, 255})
	fill(paper, color.White)
	// 10 mm grid measured from nominal finished top-left, not from raw JPEG.
	for mm := 10; mm < 100; mm += 10 {
		x := paper.Min.X + int(math.Round(float64(mm)*PixelsPerMM))
		line(x, paper.Min.Y, x, paper.Max.Y-1, 1, color.RGBA{225, 234, 242, 255})
	}
	for mm := 10; mm < 148; mm += 10 {
		y := paper.Min.Y + int(math.Round(float64(mm)*PixelsPerMM))
		line(paper.Min.X, y, paper.Max.X-1, y, 1, color.RGBA{225, 234, 242, 255})
	}
	// Nested rings + four separately labeled edges reveal asymmetry after tearing.
	for mm := 8; mm >= 0; mm-- {
		n := int(math.Round(float64(mm) * PixelsPerMM))
		c := gray
		width := 2
		if mm == 0 {
			c = red
			width = 3
		}
		if mm == 1 {
			c = orange
		}
		if mm == 2 {
			c = green
			width = 3
		}
		r := paper.Inset(n)
		frame(r, width, c)
	}
	frame(safe, 2, green)
	for mm := 0; mm <= 8; mm++ {
		n := int(math.Round(float64(mm) * PixelsPerMM))
		// White backing keeps each 1 mm number legible across the grid and rings.
		for _, pos := range []image.Point{{620, paper.Min.Y + n - 6}, {620, paper.Max.Y - 1 - n - 6}, {paper.Min.X + n - 4, 930}, {paper.Max.X - 1 - n - 4, 930}} {
			fill(image.Rect(pos.X, pos.Y, pos.X+11, pos.Y+15), color.White)
			label(pos.X, pos.Y, 1, fmt.Sprint(mm), ink)
		}
	}
	center(195, 3, "RP-108 / TRIM TEST", ink)
	center(265, 2, "CP1500 | 100 x 148 mm finished sheet", ink)
	center(310, 2, "Tear both feed tabs AFTER printing", ink)
	// The legend and content are well within the inner 8 mm ring.
	label(190, 395, 2, "RED   = nominal finished edge (0)", red)
	label(190, 440, 2, "AMBER = 1 mm inside finished edge", orange)
	label(190, 485, 2, "GREEN = APK safe area, about 2 mm", green)
	label(190, 530, 2, "GRAY  = 3, 4, 5, 6, 7, 8 mm rings", gray)
	center(620, 2, "Read the first visible ring on EACH edge.", ink)
	center(665, 2, "Compare TOP / BOTTOM / LEFT / RIGHT.", ink)
	center(710, 2, "Ruler units: approximately 1 mm per ring.", ink)
	center(755, 2, "TOP", ink)
	center(1080, 2, "BOTTOM", ink)
	label(155, 900, 2, "LEFT", ink)
	label(990, 900, 2, "RIGHT", ink)
	// Circle and crosshair diagnose scale and nonuniform stretching.
	cx, cy, radius := 624, 936, int(math.Round(10*PixelsPerMM))
	for deg := 0.0; deg < 360; deg += .15 {
		rad := deg * math.Pi / 180
		x, y := cx+int(math.Round(float64(radius)*math.Cos(rad))), cy+int(math.Round(float64(radius)*math.Sin(rad)))
		fill(image.Rect(x-1, y-1, x+2, y+2), ink)
	}
	line(cx-radius-15, cy, cx+radius+15, cy, 2, ink)
	line(cx, cy-radius-15, cx, cy+radius+15, 2, ink)
	center(1140, 2, "Circle diameter: nominal 20 mm", ink)
	// A 50 mm reference bar. Every 1 mm has a tick; every 5 mm is numbered.
	bx, by := 328, 1260
	length := int(math.Round(50 * PixelsPerMM))
	line(bx, by, bx+length, by, 3, ink)
	for mm := 0; mm <= 50; mm++ {
		x := bx + int(math.Round(float64(mm)*PixelsPerMM))
		h := 12
		if mm%5 == 0 {
			h = 24
			label(x-9, by+34, 1, fmt.Sprint(mm), ink)
		}
		line(x, by-h, x, by+h, 2, ink)
	}
	center(1350, 2, "Reference bar: nominal 50 mm", ink)
	center(1430, 2, "Raster: 1248 x 1872 (includes bleed)", ink)
	center(1475, 2, "Finished sheet: [32,60] 1184 x 1752", red)
	center(1520, 2, "Safe content: [55,84] 1138 x 1704", green)
	center(1600, 2, "Normal artwork: --area safe --fit contain", green)
	center(1650, 1, "Calibration only: print THIS page with --area bleed --fit cover", ink)
	// Corner fiducials remain inside the safe area; use them to spot lost corners.
	for _, v := range []struct {
		x, y int
		s    string
	}{{70, 100, "TL"}, {1130, 100, "TR"}, {70, 1730, "BL"}, {1130, 1730, "BR"}} {
		label(v.x, v.y, 2, v.s, green)
	}
	return im
}
