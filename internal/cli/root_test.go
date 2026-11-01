package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestDryRunOfflineAndPreviewDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	input, preview := filepath.Join(dir, "photo.png"), filepath.Join(dir, "preview.jpg")
	f, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	_ = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 16, 24)))
	f.Close()
	args := []string{"--home", dir, "--host", "192.0.2.123", "--timeout", "1ns", "--model", "CP1500", "--json", "print", input, "--dry-run", "--preview", preview}
	var out, stderr bytes.Buffer
	if err = Execute(context.Background(), "test", args, &out, &stderr); err != nil {
		t.Fatalf("%v: %s", err, &stderr)
	}
	var result map[string]any
	if err = json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["state"] != "dry-run" {
		t.Fatal(result)
	}
	if im, ok := result["image"].(map[string]any); !ok || im["area"] != "safe" {
		t.Fatal("CLI default must preserve the full composition inside the safe area")
	}
	before, err := os.ReadFile(preview)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err = Execute(context.Background(), "test", args, &out, &stderr); err == nil {
		t.Fatal("preview overwritten")
	}
	after, _ := os.ReadFile(preview)
	if !bytes.Equal(before, after) {
		t.Fatal("existing preview changed")
	}
	if !json.Valid(stderr.Bytes()) {
		t.Fatalf("error was not JSON: %s", &stderr)
	}
}
func TestConfigurationAndErrors(t *testing.T) {
	dir := t.TempDir()
	var out, stderr bytes.Buffer
	args := []string{"--home", dir, "config", "set", "--model", "CP1500", "--host", "192.0.2.1", "--json"}
	if err := Execute(context.Background(), "test", args, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm()&0077 != 0 {
		t.Fatalf("config permissions %v", stat.Mode())
	}
	out.Reset()
	stderr.Reset()
	if err := Execute(context.Background(), "test", []string{"--home", dir, "--json", "config", "show"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]string
	_ = json.Unmarshal(out.Bytes(), &cfg)
	if cfg["model"] != "CP1500" || cfg["host"] != "192.0.2.1" {
		t.Fatal(cfg)
	}
	for _, args := range [][]string{{"--json", "--timeout", "0s", "status"}, {"--json", "--format", "yaml", "papers"}, {"--json", "print"}} {
		out.Reset()
		stderr.Reset()
		if err := Execute(context.Background(), "test", args, &out, &stderr); err == nil {
			t.Fatal("invalid arguments accepted")
		}
		if !json.Valid(stderr.Bytes()) {
			t.Fatalf("invalid JSON error: %s", &stderr)
		}
	}
}

func TestSavedCalibrationOverridesAndTargetIsolation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	f, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	_ = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 10, 15)))
	f.Close()
	run := func(args ...string) map[string]any {
		t.Helper()
		var out, stderr bytes.Buffer
		base := []string{"--home", dir, "--host", "192.0.2.1", "--model", "CP1500", "--json"}
		if err := Execute(context.Background(), "test", append(base, args...), &out, &stderr); err != nil {
			t.Fatalf("%v: %s", err, &stderr)
		}
		var v map[string]any
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	run("calibrate", "set", "--offset-x-mm", "-0.8", "--offset-y-mm", "-0.5")
	plan := run("print", input, "--dry-run")
	im := plan["image"].(map[string]any)
	if im["offset_x_mm"] != -.8 || im["offset_y_mm"] != -.5 {
		t.Fatal(im)
	}
	r := im["content_rect"].(map[string]any)
	if r["x"] != float64(46) || r["y"] != float64(78) {
		t.Fatal(r)
	}
	plan = run("print", input, "--dry-run", "--offset-x-mm", "0")
	im = plan["image"].(map[string]any)
	if im["offset_x_mm"] != float64(0) || im["offset_y_mm"] != -.5 {
		t.Fatal("explicit zero must override just X", im)
	}
	plan = run("print", input, "--dry-run", "--no-calibration")
	im = plan["image"].(map[string]any)
	if im["offset_x_mm"] != float64(0) || im["offset_y_mm"] != float64(0) {
		t.Fatal("saved calibration was not bypassed")
	}
	plan = run("print", input, "--dry-run", "--host", "192.0.2.2")
	im = plan["image"].(map[string]any)
	if im["offset_x_mm"] != float64(0) {
		t.Fatal("calibration leaked to another printer")
	}
	run("calibrate", "clear")
	show := run("calibrate", "show")
	if show["saved"] != false {
		t.Fatal("calibration not cleared")
	}
}
