package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/songquanpeng/selphy-cli/internal/config"
	"github.com/songquanpeng/selphy-cli/internal/cpnp"
	"github.com/songquanpeng/selphy-cli/internal/photo"
	"github.com/spf13/cobra"
)

type imageFlags struct {
	paper, fit, preview, area    string
	rotate, quality              int
	marginMM                     float64
	offsetXMM, offsetYMM         float64
	noCalibration                bool
	command                      *cobra.Command
	guidePreview, trimmedPreview string
}

func (f *imageFlags) flags(cmd *cobra.Command) {
	f.command = cmd
	cmd.Flags().StringVar(&f.paper, "paper", "auto", "paper profile (see selphy papers; auto uses model default)")
	cmd.Flags().StringVar(&f.fit, "fit", "contain", "contain: preserve the full image; cover: center crop")
	cmd.Flags().StringVar(&f.area, "area", "safe", "content placement: safe (trim-safe), paper (finished sheet), bleed (full raster)")
	cmd.Flags().Float64Var(&f.marginMM, "margin-mm", 0, "extra inset inside the selected area, in approximate millimeters")
	cmd.Flags().IntVar(&f.rotate, "rotate", 0, "clockwise rotation in degrees, after EXIF orientation")
	cmd.Flags().IntVar(&f.quality, "quality", 95, "JPEG quality 1..100")
	cmd.Flags().BoolVar(&f.noCalibration, "no-calibration", false, "ignore the saved printer/paper calibration")
	cmd.Flags().Float64Var(&f.offsetXMM, "offset-x-mm", 0, "horizontal placement correction; negative = left, overrides saved value")
	cmd.Flags().Float64Var(&f.offsetYMM, "offset-y-mm", 0, "vertical placement correction; negative = up, overrides saved value")
	cmd.Flags().StringVar(&f.guidePreview, "preview-guides", "", "write a separate PNG with expected trim/safe lines (view only)")
	cmd.Flags().StringVar(&f.trimmedPreview, "preview-trimmed", "", "write a JPEG estimate of the finished sheet (view only)")
}
func (f *imageFlags) prepare(r *runtime, path string) (photo.Image, photo.Profile, error) {
	cfg, err := r.resolved()
	if err != nil {
		return photo.Image{}, photo.Profile{}, err
	}
	if cfg.Model == "" {
		return photo.Image{}, photo.Profile{}, fmt.Errorf("specify --model (e.g. CP1500), or save it with config set")
	}
	p, err := photo.GetProfile(cfg.Model, f.paper)
	if err != nil {
		return photo.Image{}, p, err
	}
	c, err := r.client()
	if err != nil {
		return photo.Image{}, p, err
	}
	model, err := photo.NormalizeModel(cfg.Model)
	if err != nil {
		return photo.Image{}, p, err
	}
	cal := config.Calibration{}
	calibrated := false
	if !f.noCalibration {
		cal, calibrated = cfg.Calibrations[config.CalibrationKey(c.Host, model, p.Name)]
	}
	trimCal := cal
	if f.command.Flags().Changed("offset-x-mm") {
		cal.OffsetXMM = f.offsetXMM
	}
	if f.command.Flags().Changed("offset-y-mm") {
		cal.OffsetYMM = f.offsetYMM
	}
	im, err := photo.PrepareWithOptions(path, p, photo.Options{Fit: f.fit, Area: f.area, Rotate: f.rotate, Quality: f.quality, MarginMM: f.marginMM, OffsetXMM: cal.OffsetXMM, OffsetYMM: cal.OffsetYMM, TrimOffsetXMM: trimCal.OffsetXMM, TrimOffsetYMM: trimCal.OffsetYMM, TrimCalibrated: calibrated})
	return im, p, err
}
func writePreview(path string, b []byte) error {
	// Never replace the source photo or an existing preview by accident.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	return closeErr
}
func prepareCommand(r *runtime) *cobra.Command {
	f := &imageFlags{}
	var output string
	cmd := &cobra.Command{Use: "prepare PHOTO", Short: "Render a print-ready JPEG offline", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		im, p, err := f.prepare(r, args[0])
		if err != nil {
			return err
		}
		if output == "" {
			return fmt.Errorf("--output is required")
		}
		if err = writePreview(output, im.JPEG); err != nil {
			return err
		}
		if err = f.writeAuxPreviews(im); err != nil {
			return err
		}
		return r.emit(map[string]any{"output": output, "paper": p, "image": im, "preview_guides": f.guidePreview, "preview_trimmed": f.trimmedPreview}, func() {
			fmt.Fprintf(r.out, "Wrote %s: %d×%d, %d bytes (%s)\n", output, im.Width, im.Height, im.Bytes, p.Name)
			fmt.Fprintf(r.out, "Layout: %s / %s; correction X %+.2f mm, Y %+.2f mm\n", im.Area, f.fit, im.OffsetXMM, im.OffsetYMM)
			writeTrimInfo(r.out, im, f)
		})
	}}
	f.flags(cmd)
	cmd.Flags().StringVarP(&output, "output", "o", "", "output JPEG path (must not exist)")
	return cmd
}
func printCommand(r *runtime) *cobra.Command {
	f := &imageFlags{}
	var dryRun, wait bool
	var jobTimeout time.Duration
	cmd := &cobra.Command{Use: "print PHOTO", Short: "Print one photo (consumes paper and ink)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if jobTimeout <= 0 {
			return fmt.Errorf("--job-timeout must be positive")
		}
		im, p, err := f.prepare(r, args[0])
		if err != nil {
			return err
		}
		cfg, err := r.resolved()
		if err != nil {
			return err
		}
		if f.preview != "" {
			if err = writePreview(f.preview, im.JPEG); err != nil {
				return err
			}
		}
		if err = f.writeAuxPreviews(im); err != nil {
			return err
		}
		c, err := r.client()
		if err != nil {
			return err
		}
		plan := map[string]any{"state": "dry-run", "host": c.Host, "model": cfg.Model, "paper": p, "image": im, "sha256": fmt.Sprintf("%x", sha256.Sum256(im.JPEG)), "preview": f.preview, "preview_guides": f.guidePreview, "preview_trimmed": f.trimmedPreview}
		if dryRun {
			return r.emit(plan, func() {
				fmt.Fprintf(r.out, "Dry run: %s at %s, %s, %d×%d, %d bytes\n", cfg.Model, c.Host, p.Name, im.Width, im.Height, im.Bytes)
				fmt.Fprintf(r.out, "Layout: %s / %s; correction X %+.2f mm, Y %+.2f mm\n", im.Area, f.fit, im.OffsetXMM, im.OffsetYMM)
				writeTrimInfo(r.out, im, f)
				if f.preview != "" {
					fmt.Fprintf(r.out, "Preview: %s\n", f.preview)
				}
			})
		}
		// Explain trim before contacting the printer. This never gates printing
		// behind a prompt, and stdout remains machine-readable under --json.
		writeTrimInfo(r.errOut, im, f)
		ctx, cancel := context.WithTimeout(cmd.Context(), jobTimeout)
		defer cancel()
		status, err := c.Status(ctx)
		if err != nil {
			return err
		}
		if status.ErrorCode != 0 {
			return fmt.Errorf("printer: %s", status.Error)
		}
		if status.StateCode != 1 {
			return fmt.Errorf("printer is %s; wait until idle", status.State)
		}
		if !status.JPEG {
			return fmt.Errorf("printer does not advertise JPEG printing")
		}
		if im.Width < int(status.MinWidth) || im.Height < int(status.MinHeight) || im.Width > int(status.MaxWidth) || im.Height > int(status.MaxHeight) {
			return fmt.Errorf("prepared image %d×%d outside printer limits %d×%d..%d×%d", im.Width, im.Height, status.MinWidth, status.MinHeight, status.MaxWidth, status.MaxHeight)
		}
		if err = checkMedia(p, status); err != nil {
			return err
		}
		id, err := c.DeviceID(ctx)
		if err != nil {
			return fmt.Errorf("verify printer model: %w", err)
		}
		actual := strings.ToUpper(id["MDL"])
		expected, err := photo.NormalizeModel(cfg.Model)
		if err != nil {
			return err
		}
		if actual == "" || !strings.Contains(actual, expected) {
			return fmt.Errorf("printer reports model %q; requested %s", actual, expected)
		}
		fmt.Fprintf(r.errOut, "Layout: %s / %s; correction X %+.2f mm, Y %+.2f mm\n", im.Area, f.fit, im.OffsetXMM, im.OffsetYMM)
		result, err := c.Print(ctx, cpnp.Page{JPEG: im.JPEG, Width: im.Width, Height: im.Height, Size: p.Code, CP: p.Family == "CP", Optimize: true}, func(s cpnp.ConnectedStatus) {
			fmt.Fprintf(r.errOut, "Printer: %s; request=0x%x; JPEG offset=%d length=%d\n", s.State, s.Request, s.Offset, s.Length)
		})
		if err != nil {
			return fmt.Errorf("print: %w (do not retry blindly; inspect printer and output first)", err)
		}
		if wait && result.State == "submitted" {
			fmt.Fprintln(r.errOut, "Job submitted; waiting for printer to return to idle...")
			if err = waitIdle(ctx, c); err != nil {
				return fmt.Errorf("job submitted; completion unknown: %w", err)
			}
			result.State = "idle-after-submission"
		}
		return r.emit(map[string]any{"state": result.State, "host": c.Host, "model": expected, "paper": p.Name, "image": im, "sha256": plan["sha256"], "preview_guides": f.guidePreview, "preview_trimmed": f.trimmedPreview}, func() { fmt.Fprintf(r.out, "%s: %s (%s, %d×%d)\n", result.State, c.Host, p.Name, im.Width, im.Height) })
	}}
	f.flags(cmd)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "prepare and describe the job without network access or printing")
	cmd.Flags().StringVar(&f.preview, "preview", "", "write the exact JPEG to this new path")
	cmd.Flags().BoolVar(&wait, "wait", false, "after CP submission, wait for printer idle (not proof of physical output)")
	cmd.Flags().DurationVar(&jobTimeout, "job-timeout", 3*time.Minute, "overall print deadline")
	return cmd
}
func checkMedia(p photo.Profile, s cpnp.Status) error {
	ink := p.Name
	if p.Name == "postcard" {
		ink = "cp-postcard"
	} else if p.Name == "card" {
		ink = "cp-card"
	} else if p.Name == "l" {
		ink = "cp-l"
	} else if p.Name == "qx-square-borderless" {
		ink = "qx-square"
	}
	if s.Ink != ink && !(ink == "cp-postcard" && strings.HasPrefix(s.Ink, "cp-postcard-")) {
		return fmt.Errorf("paper profile %s requires %s ink; installed %s", p.Name, ink, s.Ink)
	}
	if p.Family == "CP" && s.Tray != ink {
		return fmt.Errorf("paper profile %s requires tray %s; installed %s", p.Name, ink, s.Tray)
	}
	return nil
}
func waitIdle(ctx context.Context, c *cpnp.Client) error {
	// Allow the printer time to transition out of the submitted state.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
	}
	for {
		s, err := c.Status(ctx)
		if err != nil {
			return err
		}
		if s.ErrorCode != 0 {
			return fmt.Errorf("printer: %s", s.Error)
		}
		if s.StateCode == 1 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
