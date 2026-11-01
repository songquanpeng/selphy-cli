package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/songquanpeng/selphy-cli/internal/config"
	"github.com/songquanpeng/selphy-cli/internal/cpnp"
	"github.com/songquanpeng/selphy-cli/internal/photo"
	"github.com/spf13/cobra"
)

type runtime struct {
	out, errOut               io.Writer
	host, model, home, format string
	json                      bool
	timeout                   time.Duration
	port                      int
}

func Execute(ctx context.Context, version string, args []string, out, errOut io.Writer) error {
	r := &runtime{out: out, errOut: errOut}
	root := newRoot(r, version)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil {
		if r.json || r.format == "json" {
			_ = json.NewEncoder(errOut).Encode(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(errOut, "Error:", err)
		}
	}
	return err
}
func (r *runtime) emit(v any, text func()) error {
	if r.json || r.format == "json" {
		e := json.NewEncoder(r.out)
		e.SetIndent("", "  ")
		e.SetEscapeHTML(false)
		return e.Encode(v)
	}
	text()
	return nil
}
func (r *runtime) resolved() (config.Config, error) {
	cfg, err := config.Load(r.home)
	if err != nil {
		return cfg, err
	}
	if v := os.Getenv("SELPHY_HOST"); v != "" {
		cfg.Host = v
	}
	if r.host != "" {
		cfg.Host = r.host
	}
	if v := os.Getenv("SELPHY_MODEL"); v != "" {
		cfg.Model = v
	}
	if r.model != "" {
		cfg.Model = r.model
	}
	return cfg, nil
}
func (r *runtime) client() (*cpnp.Client, error) {
	cfg, err := r.resolved()
	if err != nil {
		return nil, err
	}
	if cfg.Host == "" {
		cfg.Host = "192.168.0.1"
	}
	if strings.ContainsAny(cfg.Host, "/\x00 \t\r\n") || strings.Contains(cfg.Host, ":") {
		return nil, fmt.Errorf("--host must be an IPv4 address or hostname, without URL scheme or port")
	}
	c := cpnp.New(cfg.Host, r.timeout)
	c.Port = r.port
	return c, nil
}
func newRoot(r *runtime, version string) *cobra.Command {
	root := &cobra.Command{Use: "selphy", Short: "Print photos on Canon SELPHY from your terminal", Long: "A pure Go SELPHY client reconstructed from Photo Layout 4.4.0.\nConnect your computer to the printer's Wi-Fi or the same LAN first.\nCP jobs report queue acceptance; QX jobs wait for the printer's end request.", Version: version, SilenceUsage: true, SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if r.format != "text" && r.format != "json" {
				return fmt.Errorf("--format must be text or json")
			}
			if r.timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}
			if r.port < 1 || r.port > 65535 {
				return fmt.Errorf("--port must be 1..65535")
			}
			return nil
		}}
	root.SetOut(r.out)
	root.SetErr(r.errOut)
	f := root.PersistentFlags()
	f.StringVar(&r.host, "host", "", "printer IP or hostname (env: SELPHY_HOST; direct Wi-Fi default: 192.168.0.1)")
	f.StringVar(&r.model, "model", "", "printer model, e.g. CP1500 (env: SELPHY_MODEL)")
	f.StringVar(&r.home, "home", "", "configuration directory (env: SELPHY_HOME)")
	f.StringVar(&r.format, "format", "text", "output format: text or json")
	f.BoolVar(&r.json, "json", false, "machine-readable JSON")
	f.DurationVar(&r.timeout, "timeout", 2*time.Second, "timeout per network exchange")
	f.IntVar(&r.port, "port", cpnp.Port, "CPNP UDP port")
	root.AddCommand(discoverCommand(r), statusCommand(r), infoCommand(r), printCommand(r), prepareCommand(r), testPageCommand(r), calibrationCommand(r), configCommand(r), doctorCommand(r), pairCommand(r))
	root.AddCommand(&cobra.Command{Use: "papers", Short: "List paper profiles and exact JPEG dimensions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return r.emit(photo.Profiles, func() {
			for _, p := range photo.Profiles {
				fmt.Fprintf(r.out, "%-24s %4d × %-4d  %s\n", p.Name, p.Width, p.Height, p.Family)
			}
		})
	}})
	root.AddCommand(&cobra.Command{Use: "agent-guide", Short: "Print CLI usage guidance for agents", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s := `Use selphy discover --json to find printers. Read selphy status --host IP --json before printing.
Use selphy config set --host IP --model CP1500 to save the target.
Use selphy print PHOTO --dry-run --preview preview.jpg --json to inspect offline.
Use --preview-guides guides.png for red estimated trim/green safe lines, and --preview-trimmed trimmed.jpg for the finished-sheet estimate.
The annotated/trimmed previews are view-only; the printer always receives the unmarked full raster.
The default --area safe --fit contain preserves the whole composition inside the trim-safe area.
Use --area bleed --fit cover for intentional borderless/cropped photos or the calibration test page.
Saved placement calibration is specific to host/model/paper. Use --no-calibration for a fresh baseline chart.
Use selphy print PHOTO to physically print ONE photo. Each invocation consumes paper and ink.
Print is never automatically retried after an ambiguous network failure: inspect printer state first.
CP result "submitted" means spool acceptance, not physical completion. Use --wait to monitor idle.
QX result "completed" means the printer sent END_PRINT.
Use --json for stable structured output. Progress goes to stderr. Exit 0 = success, 1 = error.
JPEG/PNG/WebP input; EXIF orientation is applied. HEIC can be converted using macOS sips.
No Canon login, cloud connection, APK, Java, Android, or driver is needed at runtime.
`
		return r.emit(map[string]string{"guide": s}, func() { fmt.Fprint(r.out, s) })
	}})
	return root
}

func discoverCommand(r *runtime) *cobra.Command {
	var duration time.Duration
	var targets []string
	cmd := &cobra.Command{Use: "discover", Short: "Discover printers using UDP CPNP NIC queries", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if duration <= 0 {
			return fmt.Errorf("--duration must be positive")
		}
		if r.host != "" {
			targets = append(targets, r.host)
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), duration)
		defer cancel()
		ds, err := cpnp.Discover(ctx, targets, r.port)
		if err != nil {
			return err
		}
		return r.emit(ds, func() {
			if len(ds) == 0 {
				fmt.Fprintln(r.out, "No CPNP printers found. Connect to the printer's Wi-Fi or specify --target IP.")
			}
			for _, d := range ds {
				fmt.Fprintf(r.out, "%s  %s  %s\n", d.Host, d.MAC, d.Protocol)
			}
		})
	}}
	cmd.Flags().DurationVar(&duration, "duration", 3*time.Second, "discovery window")
	cmd.Flags().StringSliceVar(&targets, "target", nil, "query specific IP(s) instead of broadcasts")
	return cmd
}
func statusCommand(r *runtime) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Read printer state, paper/ink, battery and image limits", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := r.client()
		if err != nil {
			return err
		}
		s, err := c.Status(cmd.Context())
		if err != nil {
			return err
		}
		return r.emit(s, func() {
			fmt.Fprintf(r.out, "%s: %s\nPaper tray: %s  Ink: %s\nError: %s\nPower: %s  Battery level: %d (0–4; 255 = unknown)\nJPEG: %t  %d×%d .. %d×%d\nFirmware: %s\n", c.Host, s.State, s.Tray, s.Ink, s.Error, s.Power, s.BatteryLevel, s.JPEG, s.MinWidth, s.MinHeight, s.MaxWidth, s.MaxHeight, s.Firmware)
		})
	}}
}
func infoCommand(r *runtime) *cobra.Command {
	return &cobra.Command{Use: "info", Short: "Read IEEE 1284 device identity", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := r.client()
		if err != nil {
			return err
		}
		id, err := c.DeviceID(cmd.Context())
		if err != nil {
			return err
		}
		return r.emit(id, func() { b, _ := json.MarshalIndent(id, "", "  "); fmt.Fprintln(r.out, string(b)) })
	}}
}
func configCommand(r *runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Store a default printer (no credentials)"}
	cmd.AddCommand(&cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := r.resolved()
		if err != nil {
			return err
		}
		return r.emit(cfg, func() { fmt.Fprintf(r.out, "Host: %s\nModel: %s\n", cfg.Host, cfg.Model) })
	}})
	cmd.AddCommand(&cobra.Command{Use: "set", Short: "Save --host and --model as defaults", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := r.resolved()
		if err != nil {
			return err
		}
		if cfg.Host == "" || cfg.Model == "" {
			return fmt.Errorf("specify --host and --model")
		}
		cfg.Model, err = photo.NormalizeModel(cfg.Model)
		if err != nil {
			return err
		}
		if _, err = r.client(); err != nil {
			return err
		}
		if err = config.Save(r.home, cfg); err != nil {
			return err
		}
		return r.emit(cfg, func() { fmt.Fprintf(r.out, "Saved %s at %s\n", cfg.Model, cfg.Host) })
	}})
	return cmd
}
func doctorCommand(r *runtime) *cobra.Command {
	var checkSession bool
	cmd := &cobra.Command{Use: "doctor", Short: "Check local network interfaces and printer reachability", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := r.client()
		if err != nil {
			return err
		}
		addrs, _ := net.InterfaceAddrs()
		networks := []string{}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				networks = append(networks, n.String())
			}
		}
		s, probeErr := c.Status(cmd.Context())
		report := map[string]any{"host": c.Host, "port": c.Port, "networks": networks, "reachable": probeErr == nil, "status": s}
		if probeErr == nil && checkSession {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			session, e := c.Open(ctx)
			if e == nil {
				ready, readyErr := session.Ready(ctx)
				closeErr := session.Close()
				if readyErr != nil {
					e = readyErr
				} else if closeErr != nil {
					e = closeErr
				} else {
					report["session"] = ready
					report["session_ready"] = true
				}
			}
			if e != nil {
				probeErr = e
				report["session_ready"] = false
			}
		}
		if probeErr != nil {
			report["error"] = probeErr.Error()
		}
		if err = r.emit(report, func() {
			fmt.Fprintf(r.out, "Target: %s:%d\nNetworks: %s\n", c.Host, c.Port, strings.Join(networks, ", "))
			if probeErr != nil {
				fmt.Fprintln(r.out, probeErr)
			} else {
				fmt.Fprintf(r.out, "Printer reachable: %s (%s)\n", s.State, s.Ink)
				if checkSession {
					fmt.Fprintln(r.out, "Print session ready; no job sent.")
				}
			}
		}); err != nil {
			return err
		}
		if probeErr != nil {
			return fmt.Errorf("network or print-session diagnostic failed; see diagnostic output")
		}
		return nil
	}}
	cmd.Flags().BoolVar(&checkSession, "session", false, "also negotiate and release a print session without sending a job")
	return cmd
}
