package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/songquanpeng/selphy-cli/internal/config"
)

func fixture(t *testing.T) (*Service, string, Request) {
	t.Helper()
	s, err := New(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	path := filepath.Join(t.TempDir(), "test.png")
	im := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			im.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, im); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return s, path, Request{Host: "printer.local", Model: "CP1500", Paper: "postcard", Area: "safe", Fit: "contain", Quality: 95}
}
func selectPrepare(t *testing.T, s *Service, path string, r Request) Preview {
	t.Helper()
	if _, err := s.Select(path); err != nil {
		t.Fatal(err)
	}
	p, err := s.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func arg(args []string, key string) string {
	for i, v := range args {
		if v == key && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestOfflinePreviewAndExport(t *testing.T) {
	s, path, r := fixture(t)
	p := selectPrepare(t, s, path, r)
	if p.ID == "" || p.Image.Width != 1248 || p.Image.Height != 1872 || p.Image.TrimBasis != "nominal-model" {
		t.Fatalf("bad preview: %+v", p.Image)
	}
	for _, v := range []string{p.Full, p.Guides, p.Trimmed} {
		if !strings.HasPrefix(v, "data:image/") || len(v) < 100 {
			t.Fatal("missing image")
		}
	}
	out := filepath.Join(t.TempDir(), "print.jpg")
	if err := s.Export(p.ID, out); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(out)
	if !bytes.HasPrefix(first, []byte{0xff, 0xd8}) {
		t.Fatal("not JPEG")
	}
	if err := s.Export(p.ID, out); err == nil {
		t.Fatal("overwrote existing file")
	}
	again, _ := os.ReadFile(out)
	if !bytes.Equal(first, again) {
		t.Fatal("export was modified")
	}
}
func TestPrintUsesFrozenSourceCalibrationAndOriginalCLI(t *testing.T) {
	s, path, r := fixture(t)
	cfg := config.Config{Host: r.Host, Model: r.Model, Calibrations: map[string]config.Calibration{config.CalibrationKey(r.Host, r.Model, r.Paper): {OffsetXMM: -0.8, OffsetYMM: -0.5}}}
	if err := config.Save(s.home, cfg); err != nil {
		t.Fatal(err)
	}
	p := selectPrepare(t, s, path, r)
	if p.Image.TrimBasis != "saved-calibration" || p.Image.OffsetXMM != -0.8 {
		t.Fatal("calibration not applied")
	}
	source, _ := os.ReadFile(s.source)
	if err := os.WriteFile(path, []byte("original changed"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.Calibrations = nil
	if err := config.Save(s.home, cfg); err != nil {
		t.Fatal(err)
	}
	called := 0
	s.run = func(ctx context.Context, version string, args []string, out, log io.Writer) error {
		called++
		if args[0] != "print" || args[1] != s.source {
			t.Fatal("not original snapshot")
		}
		current, _ := os.ReadFile(args[1])
		if !bytes.Equal(current, source) {
			t.Fatal("source changed")
		}
		frozen, err := config.Load(arg(args, "--home"))
		if err != nil {
			t.Fatal(err)
		}
		if len(frozen.Calibrations) != 1 {
			t.Fatal("calibration not frozen")
		}
		if arg(args, "--area") != "safe" || arg(args, "--quality") != "95" || !reflect.DeepEqual(args[len(args)-2:], []string{"--wait", "--json"}) {
			t.Fatalf("bad print arguments: %v", args)
		}
		if strings.Contains(strings.Join(args, " "), "--preview") || strings.Contains(strings.Join(args, " "), "--dry-run") {
			t.Fatal("preview arguments leaked")
		}
		io.WriteString(log, "Printer: preparing\n")
		io.WriteString(out, `{"state":"idle-after-submission"}`)
		return nil
	}
	var progress bytes.Buffer
	if _, err := s.Print(p.ID, &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Len() == 0 {
		t.Fatal("no progress")
	}
	if _, err := s.Print(p.ID, nil); err == nil || called != 1 {
		t.Fatal("duplicate submission allowed")
	}
}
func TestAmbiguousFailureConsumesPreview(t *testing.T) {
	s, path, r := fixture(t)
	p := selectPrepare(t, s, path, r)
	calls := 0
	s.run = func(context.Context, string, []string, io.Writer, io.Writer) error {
		calls++
		return errors.New("connection lost after upload")
	}
	if _, err := s.Print(p.ID, nil); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := s.Print(p.ID, nil); err == nil || calls != 1 {
		t.Fatal("automatic retry possible")
	}
}
func TestNewPreviewInvalidatesOldAndRejectsInvalidLayout(t *testing.T) {
	s, path, r := fixture(t)
	p := selectPrepare(t, s, path, r)
	r.Rotate = 90
	q, err := s.Prepare(r)
	if err != nil || q.ID == p.ID {
		t.Fatal(err)
	}
	if _, err = s.Print(p.ID, nil); err == nil {
		t.Fatal("accepted stale ID")
	}
	r.Paper = "qx-card"
	if _, err = s.Prepare(r); err == nil {
		t.Fatal("accepted incompatible paper")
	}
	if _, err = s.Print(q.ID, nil); err == nil {
		t.Fatal("invalid settings left a printable preview")
	}
}
func TestConfigCalibrationPersistenceAndIsolation(t *testing.T) {
	s, path, r := fixture(t)
	selectPrepare(t, s, path, r)
	if err := s.SaveTarget(r.Host, r.Model); err != nil {
		t.Fatal(err)
	}
	if s.ready != nil {
		t.Fatal("saved config did not invalidate preview")
	}
	if err := s.Calibrate(r, -0.8, -0.5); err != nil {
		t.Fatal(err)
	}
	p, err := s.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	if p.Image.OffsetXMM != -0.8 {
		t.Fatal("offset missing")
	}
	r.Paper = "l"
	p, err = s.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	if p.Image.OffsetXMM != 0 || p.Image.TrimBasis != "nominal-model" {
		t.Fatal("calibration leaked to other paper")
	}
	cfg, err := s.Config()
	if err != nil || cfg.Host != "printer.local" || cfg.Model != "CP1500" {
		t.Fatalf("%+v %v", cfg, err)
	}
}
func TestBusyCancelAndCleanup(t *testing.T) {
	s, path, r := fixture(t)
	p := selectPrepare(t, s, path, r)
	started, finished := make(chan struct{}), make(chan error, 1)
	var calls atomic.Int32
	s.run = func(ctx context.Context, _ string, _ []string, _, _ io.Writer) error {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	go func() { _, err := s.Print(p.ID, nil); finished <- err }()
	<-started
	if _, err := s.Print(p.ID, nil); err == nil {
		t.Fatal("concurrent print accepted")
	}
	if _, err := s.Select(path); err == nil {
		t.Fatal("concurrent selection accepted")
	}
	s.Close()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not propagate")
	}
	if calls.Load() != 1 {
		t.Fatal("multiple submissions")
	}
	if _, err := os.Stat(s.dir); !os.IsNotExist(err) {
		t.Fatal("temporary files not cleaned")
	}
	if _, err := s.Config(); err == nil {
		t.Fatal("closed service accepted operations")
	}
}
func TestValidationDoesNotExecuteCommands(t *testing.T) {
	s, _, r := fixture(t)
	s.run = func(context.Context, string, []string, io.Writer, io.Writer) error {
		t.Fatal("unexpected command")
		return nil
	}
	for _, host := range []string{"", "http://printer", "host:9000", "--help", "printer name"} {
		if err := s.SaveTarget(host, "CP1500"); err == nil {
			t.Fatalf("accepted host %q", host)
		}
	}
	if _, err := s.Query("print", r.Host, r.Model); err == nil {
		t.Fatal("query can print")
	}
	if _, err := s.Print("invalid", nil); err == nil {
		t.Fatal("printed without preview")
	}
	if err := s.Export("invalid", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("exported without preview")
	}
}
func TestInvalidPhotoPreservesSelectedSnapshot(t *testing.T) {
	s, path, r := fixture(t)
	p := selectPrepare(t, s, path, r)
	old := s.source
	invalid := filepath.Join(t.TempDir(), "bad.jpg")
	os.WriteFile(invalid, []byte("not a photo"), 0600)
	if _, err := s.Select(invalid); err == nil {
		t.Fatal("accepted corrupt photo")
	}
	if s.source != old || s.ready.id != p.ID {
		t.Fatal("cancelled/failed selection discarded good preview")
	}
	if _, err := s.Select(path + ".txt"); err == nil {
		t.Fatal("accepted unsupported format")
	}
}
func TestJSONResults(t *testing.T) {
	s, _, _ := fixture(t)
	s.run = func(_ context.Context, _ string, _ []string, out, _ io.Writer) error {
		io.WriteString(out, `{"state":"idle"}`)
		return nil
	}
	result, err := s.Query("status", "printer.local", "CP1500")
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]string
	if err = json.Unmarshal(result.Data, &status); err != nil || status["state"] != "idle" {
		t.Fatal("not structured JSON")
	}
}

func TestHEICImportOnMac(t *testing.T) {
	if _, err := exec.LookPath("sips"); err != nil {
		t.Skip("macOS sips is required")
	}
	s, path, r := fixture(t)
	heic := filepath.Join(t.TempDir(), "photo.heic")
	if b, err := exec.Command("sips", "-s", "format", "heic", path, "--out", heic).CombinedOutput(); err != nil {
		t.Fatalf("create HEIC: %s: %v", b, err)
	}
	p := selectPrepare(t, s, heic, r)
	if p.Name != "photo.heic" || p.Image.SourceWidth != 320 || p.Image.SourceHeight != 240 {
		t.Fatal("HEIC did not import")
	}
}

func TestConfirmationUsesFrozenTarget(t *testing.T) {
	s, path, r := fixture(t)
	p := selectPrepare(t, s, path, r)
	message, err := s.Confirmation(p.ID)
	if err != nil || !strings.Contains(message, "printer.local") || !strings.Contains(message, "test.png") || !strings.Contains(message, "postcard") {
		t.Fatalf("%s %v", message, err)
	}
	if _, err := s.Confirmation("stale"); err == nil {
		t.Fatal("stale preview can be confirmed")
	}
}
