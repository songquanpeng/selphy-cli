package cli

import (
	"fmt"

	"github.com/songquanpeng/selphy-cli/internal/config"
	"github.com/songquanpeng/selphy-cli/internal/photo"
	"github.com/spf13/cobra"
)

func calibrationCommand(r *runtime) *cobra.Command {
	var paper string
	cmd := &cobra.Command{Use: "calibrate", Short: "Save placement offsets for this printer, model and paper"}
	cmd.PersistentFlags().StringVar(&paper, "paper", "auto", "paper profile to calibrate")
	resolve := func() (config.Config, string, error) {
		cfg, err := r.resolved()
		if err != nil {
			return cfg, "", err
		}
		model, err := photo.NormalizeModel(cfg.Model)
		if err != nil {
			return cfg, "", err
		}
		p, err := photo.GetProfile(model, paper)
		if err != nil {
			return cfg, "", err
		}
		c, err := r.client()
		if err != nil {
			return cfg, "", err
		}
		return cfg, config.CalibrationKey(c.Host, model, p.Name), nil
	}
	cmd.AddCommand(&cobra.Command{Use: "show", Short: "Show saved offsets for the selected target and paper", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, key, err := resolve()
		if err != nil {
			return err
		}
		cal, ok := cfg.Calibrations[key]
		return r.emit(map[string]any{"key": key, "saved": ok, "calibration": cal}, func() {
			fmt.Fprintf(r.out, "%s\nX: %.2f mm (negative = left)\nY: %.2f mm (negative = up)\n%s\n", key, cal.OffsetXMM, cal.OffsetYMM, cal.Note)
		})
	}})
	var x, y float64
	var note string
	set := &cobra.Command{Use: "set", Short: "Save manual correction; negative X moves left, negative Y moves up", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, key, err := resolve()
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("offset-x-mm") && !cmd.Flags().Changed("offset-y-mm") {
			return fmt.Errorf("specify --offset-x-mm and/or --offset-y-mm")
		}
		cal := cfg.Calibrations[key]
		if cmd.Flags().Changed("offset-x-mm") {
			cal.OffsetXMM = x
		}
		if cmd.Flags().Changed("offset-y-mm") {
			cal.OffsetYMM = y
		}
		if cmd.Flags().Changed("note") {
			cal.Note = note
		}
		if _, err = photo.OffsetPixels(cal.OffsetXMM, cal.OffsetYMM); err != nil {
			return err
		}
		if cfg.Calibrations == nil {
			cfg.Calibrations = map[string]config.Calibration{}
		}
		cfg.Calibrations[key] = cal
		if err = config.Save(r.home, cfg); err != nil {
			return err
		}
		return r.emit(map[string]any{"key": key, "calibration": cal}, func() {
			fmt.Fprintf(r.out, "Saved %s: X %.2f mm, Y %.2f mm. Future print/prepare commands apply these offsets.\n", key, cal.OffsetXMM, cal.OffsetYMM)
		})
	}}
	set.Flags().Float64Var(&x, "offset-x-mm", 0, "negative = left; positive = right")
	set.Flags().Float64Var(&y, "offset-y-mm", 0, "negative = up; positive = down")
	set.Flags().StringVar(&note, "note", "", "optional calibration note (stored locally)")
	cmd.AddCommand(set)
	cmd.AddCommand(&cobra.Command{Use: "clear", Short: "Remove calibration for the selected target and paper", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, key, err := resolve()
		if err != nil {
			return err
		}
		delete(cfg.Calibrations, key)
		if err = config.Save(r.home, cfg); err != nil {
			return err
		}
		return r.emit(map[string]any{"key": key, "cleared": true}, func() { fmt.Fprintln(r.out, "Cleared", key) })
	}})
	return cmd
}
