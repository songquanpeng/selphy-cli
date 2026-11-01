package photo

import (
	"fmt"
	"image"
	"math"
)

const PixelsPerMM = 11.835 // printer_support.json

type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func rect(r image.Rectangle) Rect { return Rect{r.Min.X, r.Min.Y, r.Dx(), r.Dy()} }

// PaperBounds maps the finished sheet into the full JPEG raster. QX uses its
// printable image region, since its physical sheet includes an asymmetric tab.
func PaperBounds(p Profile) image.Rectangle {
	switch p.Name {
	case "postcard":
		return image.Rect(32, 60, 1216, 1812)
	case "l":
		return image.Rect(49, 31, 1103, 1440)
	case "card":
		return image.Rect(35, 14, 1053, 654)
	default:
		return image.Rect(0, 0, p.Width, p.Height)
	}
}
func SafeBounds(p Profile) image.Rectangle {
	switch p.Name {
	case "postcard":
		return image.Rect(55, 84, 1193, 1788)
	case "l":
		return image.Rect(73, 56, 1079, 1416)
	case "card":
		return image.Rect(46, 28, 1042, 640)
	case "qx-square-borderless":
		return image.Rect(34, 15, 814, 811)
	case "qx-card":
		return image.Rect(34, 15, 610, 811)
	default:
		return image.Rect(0, 0, p.Width, p.Height)
	}
}
func ContentBounds(p Profile, area string, marginMM float64) (image.Rectangle, error) {
	var r image.Rectangle
	switch area {
	case "bleed":
		r = image.Rect(0, 0, p.Width, p.Height)
	case "paper":
		r = PaperBounds(p)
	case "safe":
		r = SafeBounds(p)
	default:
		return r, fmt.Errorf("--area must be safe, paper or bleed")
	}
	if math.IsNaN(marginMM) || math.IsInf(marginMM, 0) || marginMM < 0 || marginMM > 100 {
		return r, fmt.Errorf("--margin-mm must be finite and between 0 and 100")
	}
	n := int(math.Round(marginMM * PixelsPerMM))
	r = r.Inset(n)
	if r.Empty() {
		return r, fmt.Errorf("--margin-mm leaves no printable area")
	}
	return r, nil
}
