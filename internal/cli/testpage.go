package cli

import (
	"fmt"
	"image/png"
	"os"

	"github.com/songquanpeng/selphy-cli/internal/photo"
	"github.com/spf13/cobra"
)

func testPageCommand(r *runtime) *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "test-page", Short: "Generate an RP-108 trim/border calibration chart offline", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if output == "" {
			return fmt.Errorf("--output is required")
		}
		f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		err = png.Encode(f, photo.TestPage())
		closeErr := f.Close()
		if err != nil {
			os.Remove(output)
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return r.emit(map[string]any{"output": output, "width": 1248, "height": 1872, "printed": false, "print_area": "bleed", "print_fit": "cover"}, func() {
			fmt.Fprintf(r.out, "Wrote %s (1248×1872 PNG). Print this calibration with --area bleed --fit cover.\n", output)
		})
	}}
	cmd.Flags().StringVarP(&output, "output", "o", "", "new output PNG path")
	return cmd
}
