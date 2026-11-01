package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// QX QR codes contain Wi-Fi credentials; they are decoded locally, never saved.
func pairCommand(r *runtime) *cobra.Command {
	var file string
	var show bool
	cmd := &cobra.Command{Use: "pair", Short: "Decode QX10/QX20 SPLQR Wi-Fi credentials from a file or stdin", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var input io.Reader = cmd.InOrStdin()
		if file != "" {
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			defer f.Close()
			input = f
		}
		b, err := io.ReadAll(io.LimitReader(input, 4097))
		if err != nil {
			return err
		}
		if len(b) > 4096 {
			return fmt.Errorf("QR input too large")
		}
		s := strings.TrimSpace(string(b))
		if !strings.HasPrefix(s, "SPLQR:") {
			return fmt.Errorf("expected SPLQR: QR text from QX10 or QX20")
		}
		parts := []string{}
		for _, part := range strings.Split(strings.TrimPrefix(s, "SPLQR:"), "$") {
			if part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) != 4 {
			return fmt.Errorf("invalid QX QR field count")
		}
		model, version, suffix, upperLen, lowerLen := parts[1], "", parts[2], 0, 0
		switch model {
		case "QX10":
			version, upperLen, lowerLen = "1.0", 6, 3
		case "QX20":
			version, upperLen, lowerLen = "1.1", 3, 6
		default:
			return fmt.Errorf("unsupported QR model %q; CP1500: join the Wi-Fi shown on the printer", model)
		}
		if parts[0] != version || len(parts[2]) != upperLen || len(parts[3]) != lowerLen {
			return fmt.Errorf("invalid QX QR version or credential lengths")
		}
		pass := parts[3] + parts[2]
		if !show {
			pass = "[hidden; use --show-password]"
		}
		return r.emit(map[string]string{"model": model, "ssid": model + "-" + suffix, "password": pass, "host": "192.168.0.1"}, func() {
			fmt.Fprintf(r.out, "Model: %s\nWi-Fi: %s-%s\nPassword: %s\nJoin this Wi-Fi in your system network settings, then use --host 192.168.0.1.\n", model, model, suffix, pass)
		})
	}}
	cmd.Flags().StringVar(&file, "file", "", "file with decoded QR text (default: stdin)")
	cmd.Flags().BoolVar(&show, "show-password", false, "include Wi-Fi password in output")
	return cmd
}
