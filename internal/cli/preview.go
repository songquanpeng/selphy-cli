package cli

import (
	"fmt"
	"io"

	"github.com/songquanpeng/selphy-cli/internal/photo"
)

func (f *imageFlags) writeAuxPreviews(im photo.Image) error {
	if f.guidePreview != "" {
		b, err := photo.TrimGuide(im)
		if err != nil {
			return err
		}
		if err = writePreview(f.guidePreview, b); err != nil {
			return err
		}
	}
	if f.trimmedPreview != "" {
		b, err := photo.TrimmedPreview(im)
		if err != nil {
			return err
		}
		if err = writePreview(f.trimmedPreview, b); err != nil {
			return err
		}
	}
	return nil
}

func writeTrimInfo(w io.Writer, im photo.Image, f *imageFlags) {
	basis := "model estimate; not physically calibrated"
	if im.TrimBasis == "saved-calibration" {
		basis = "saved printer/paper calibration; estimated"
	}
	p := im.PaperRect
	fmt.Fprintf(w, "Trim: x=%d y=%d %d×%d px (%s).\n", p.X, p.Y, p.Width, p.Height, basis)
	switch im.Area {
	case "bleed":
		fmt.Fprintln(w, "Bleed: image outside the trim line will be discarded after tearing/trimming.")
	case "paper":
		fmt.Fprintln(w, "Paper: content can reach the trim edge; keep text inside the safe area.")
	case "safe":
		if im.ContentRect.Rectangle().In(im.SafeRect.Rectangle()) {
			fmt.Fprintln(w, "Safe: placement is inside the estimated safe area.")
		} else {
			fmt.Fprintln(w, "Safe: the placement override extends outside the estimated safe area.")
		}
	}
	if im.SourceCrop.Width != im.SourceWidth || im.SourceCrop.Height != im.SourceHeight {
		fmt.Fprintf(w, "Cover: source framing is also center-cropped to %d×%d of %d×%d px before printer trim.\n", im.SourceCrop.Width, im.SourceCrop.Height, im.SourceWidth, im.SourceHeight)
	}
	if f.guidePreview != "" {
		fmt.Fprintln(w, "Trim/safe guide (view only):", f.guidePreview)
	}
	if f.trimmedPreview != "" {
		fmt.Fprintln(w, "Finished-sheet estimate (view only):", f.trimmedPreview)
	}
	if f.guidePreview == "" && f.trimmedPreview == "" {
		fmt.Fprintln(w, "View trim before printing: --dry-run --preview-guides guides.png --preview-trimmed trimmed.jpg")
	}
}
