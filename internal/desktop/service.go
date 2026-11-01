// Package desktop adapts the verified CLI to a stateful desktop workflow.
package desktop

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/songquanpeng/selphy-cli/internal/cli"
	"github.com/songquanpeng/selphy-cli/internal/config"
	"github.com/songquanpeng/selphy-cli/internal/photo"
)

type Executor func(context.Context, string, []string, io.Writer, io.Writer) error

type Request struct {
	Host    string  `json:"host"`
	Model   string  `json:"model"`
	Paper   string  `json:"paper"`
	Area    string  `json:"area"`
	Fit     string  `json:"fit"`
	Rotate  int     `json:"rotate"`
	Margin  float64 `json:"margin"`
	Quality int     `json:"quality"`
}

type Selection struct {
	Name string `json:"name"`
}

type Preview struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Full    string        `json:"full"`
	Guides  string        `json:"guides"`
	Trimmed string        `json:"trimmed"`
	Image   photo.Image   `json:"image"`
	Paper   photo.Profile `json:"paper"`
}

type Result struct {
	Data json.RawMessage `json:"data"`
	Log  string          `json:"log"`
}

type prepared struct {
	id, dir string
	args    []string
	preview Preview
	request Request
}

type Service struct {
	mu                      sync.Mutex
	ctx                     context.Context
	busy                    bool
	closed                  bool
	cancel                  context.CancelFunc
	home, dir, source, name string
	ready                   *prepared
	run                     Executor
}

func New(ctx context.Context, home string) (*Service, error) {
	dir, err := os.MkdirTemp("", "selphy-studio-")
	if err != nil {
		return nil, err
	}
	return &Service{ctx: ctx, home: home, dir: dir, run: cli.Execute}, nil
}

func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	if !s.busy {
		_ = os.RemoveAll(s.dir)
	}
}

func (s *Service) begin() (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, fmt.Errorf("工作台已关闭")
	}
	if s.busy {
		return nil, nil, fmt.Errorf("正在处理上一项操作，请稍候")
	}
	s.busy = true
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancel = cancel
	return ctx, func() {
		cancel()
		s.mu.Lock()
		s.busy = false
		s.cancel = nil
		if s.closed {
			_ = os.RemoveAll(s.dir)
		}
		s.mu.Unlock()
	}, nil
}

func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) Busy() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.busy }

func (s *Service) execute(ctx context.Context, args []string, progress io.Writer) (Result, error) {
	var out, log bytes.Buffer
	w := io.Writer(&log)
	if progress != nil {
		w = io.MultiWriter(w, progress)
	}
	err := s.run(ctx, "desktop", append(args, "--json"), &out, w)
	r := Result{Data: json.RawMessage(bytes.TrimSpace(out.Bytes())), Log: log.String()}
	if err != nil {
		return r, err
	}
	if !json.Valid(r.Data) {
		return r, fmt.Errorf("客户端收到了无效结果")
	}
	return r, nil
}

func (s *Service) Config() (config.Config, error) {
	_, done, err := s.begin()
	if err != nil {
		return config.Config{}, err
	}
	defer done()
	cfg, err := config.Load(s.home)
	if err != nil {
		return cfg, err
	}
	if v := os.Getenv("SELPHY_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("SELPHY_MODEL"); v != "" {
		cfg.Model = v
	}
	if cfg.Model == "" {
		cfg.Model = "CP1500"
	}
	if cfg.Host == "" {
		cfg.Host = "192.168.0.1"
	}
	return cfg, nil
}

func validateTarget(host, model string) error {
	if host == "" || strings.ContainsAny(host, "/:\x00 \t\r\n") || strings.HasPrefix(host, "-") {
		return fmt.Errorf("请输入打印机 IP 或主机名，不含端口和 http://")
	}
	_, err := photo.NormalizeModel(model)
	return err
}

func (s *Service) invalidate() {
	if s.ready != nil {
		_ = os.RemoveAll(s.ready.dir)
		s.ready = nil
	}
}

func (s *Service) SaveTarget(host, model string) error {
	ctx, done, err := s.begin()
	if err != nil {
		return err
	}
	defer done()
	if err = validateTarget(host, model); err != nil {
		return err
	}
	_, err = s.execute(ctx, []string{"config", "set", "--home", s.home, "--host", host, "--model", model}, nil)
	if err == nil {
		s.invalidate()
	}
	return err
}

// Select makes a private copy so later edits to the original cannot change a job.
func (s *Service) Select(path string) (Selection, error) {
	ctx, done, err := s.begin()
	if err != nil {
		return Selection{}, err
	}
	defer done()
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".heic", ".heif":
	default:
		return Selection{}, fmt.Errorf("请选择 JPEG、PNG、WebP 或 HEIC 照片")
	}
	f, err := os.Open(path)
	if err != nil {
		return Selection{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Selection{}, err
	}
	if !st.Mode().IsRegular() || st.Size() > 100<<20 {
		return Selection{}, fmt.Errorf("请选择小于 100 MB 的照片文件")
	}
	dir, err := os.MkdirTemp(s.dir, "source-")
	if err != nil {
		return Selection{}, err
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = os.RemoveAll(dir)
		}
	}()
	dest := filepath.Join(dir, "photo"+ext)
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Selection{}, err
	}
	n, err := io.Copy(out, io.LimitReader(f, (100<<20)+1))
	closeErr := out.Close()
	if err != nil {
		return Selection{}, err
	}
	if closeErr != nil {
		return Selection{}, closeErr
	}
	if n > 100<<20 {
		return Selection{}, fmt.Errorf("照片大于 100 MB")
	}
	if ext == ".heic" || ext == ".heif" {
		converted := filepath.Join(dir, "converted.jpg")
		if b, e := exec.CommandContext(ctx, "/usr/bin/sips", "-s", "format", "jpeg", dest, "--out", converted).CombinedOutput(); e != nil {
			return Selection{}, fmt.Errorf("HEIC 转换失败：%s (%w)", strings.TrimSpace(string(b)), e)
		}
		dest = converted
	}
	check, err := os.Open(dest)
	if err != nil {
		return Selection{}, err
	}
	dim, _, err := image.DecodeConfig(check)
	check.Close()
	if err != nil {
		return Selection{}, fmt.Errorf("无法读取照片：%w", err)
	}
	if dim.Width <= 0 || dim.Height <= 0 || int64(dim.Width)*int64(dim.Height) > 120_000_000 {
		return Selection{}, fmt.Errorf("照片尺寸过大，请缩小到 1.2 亿像素以内")
	}
	s.invalidate()
	if s.source != "" {
		_ = os.RemoveAll(filepath.Dir(s.source))
	}
	s.source, s.name = dest, filepath.Base(path)
	accepted = true
	return Selection{Name: s.name}, nil
}

func validateRequest(r Request) error {
	if err := validateTarget(r.Host, r.Model); err != nil {
		return err
	}
	if _, err := photo.GetProfile(r.Model, r.Paper); err != nil {
		return err
	}
	if r.Area != "safe" && r.Area != "paper" && r.Area != "bleed" {
		return fmt.Errorf("无效的排版区域")
	}
	if r.Fit != "contain" && r.Fit != "cover" {
		return fmt.Errorf("无效的缩放方式")
	}
	if r.Rotate != 0 && r.Rotate != 90 && r.Rotate != 180 && r.Rotate != 270 {
		return fmt.Errorf("旋转角度应为 0、90、180 或 270")
	}
	if r.Quality < 1 || r.Quality > 100 {
		return fmt.Errorf("JPEG 质量应为 1–100")
	}
	if r.Margin < 0 || r.Margin > 20 {
		return fmt.Errorf("留白应为 0–20 mm")
	}
	return nil
}

func (s *Service) Prepare(r Request) (Preview, error) {
	ctx, done, err := s.begin()
	if err != nil {
		return Preview{}, err
	}
	defer done()
	s.invalidate()
	if err = validateRequest(r); err != nil {
		return Preview{}, err
	}
	if s.source == "" {
		return Preview{}, fmt.Errorf("请先选择一张照片")
	}
	dir, err := os.MkdirTemp(s.dir, "preview-")
	if err != nil {
		return Preview{}, err
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = os.RemoveAll(dir)
		}
	}()
	// Freeze calibration together with the selected image and layout. Printing
	// reuses precisely these CLI arguments, never the annotated/trimmed image.
	cfg, err := config.Load(s.home)
	if err != nil {
		return Preview{}, err
	}
	cfg.Host, cfg.Model = r.Host, r.Model
	home := filepath.Join(dir, "config")
	if err = config.Save(home, cfg); err != nil {
		return Preview{}, err
	}
	args := []string{"print", s.source, "--home", home, "--host", r.Host, "--model", r.Model, "--paper", r.Paper, "--area", r.Area, "--fit", r.Fit, "--rotate", fmt.Sprint(r.Rotate), "--margin-mm", fmt.Sprint(r.Margin), "--quality", fmt.Sprint(r.Quality)}
	previewArgs := append(append([]string{}, args...), "--dry-run", "--preview", filepath.Join(dir, "full.jpg"), "--preview-guides", filepath.Join(dir, "guides.png"), "--preview-trimmed", filepath.Join(dir, "trimmed.jpg"))
	result, err := s.execute(ctx, previewArgs, nil)
	if err != nil {
		return Preview{}, err
	}
	var plan struct {
		Image photo.Image   `json:"image"`
		Paper photo.Profile `json:"paper"`
	}
	if err = json.Unmarshal(result.Data, &plan); err != nil {
		return Preview{}, err
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return Preview{}, err
	}
	p := Preview{ID: hex.EncodeToString(token[:]), Name: s.name, Image: plan.Image, Paper: plan.Paper}
	for _, item := range []struct {
		name, mime string
		target     *string
	}{{"full.jpg", "image/jpeg", &p.Full}, {"guides.png", "image/png", &p.Guides}, {"trimmed.jpg", "image/jpeg", &p.Trimmed}} {
		b, e := os.ReadFile(filepath.Join(dir, item.name))
		if e != nil {
			return Preview{}, e
		}
		*item.target = "data:" + item.mime + ";base64," + base64.StdEncoding.EncodeToString(b)
	}
	s.ready = &prepared{id: p.ID, dir: dir, args: args, preview: p, request: r}
	accepted = true
	return p, nil
}

func (s *Service) Print(id string, progress io.Writer) (Result, error) {
	ctx, done, err := s.begin()
	if err != nil {
		return Result{}, err
	}
	defer done()
	if s.ready == nil || s.ready.id != id {
		return Result{}, fmt.Errorf("预览已失效，请重新生成后打印")
	}
	p := s.ready
	// Consume before the network call: even an ambiguous failure requires a
	// fresh preview and explicit confirmation before another submission.
	s.ready = nil
	defer os.RemoveAll(p.dir)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	return s.execute(ctx, append(p.args, "--wait"), progress)
}

func (s *Service) Confirmation(id string) (string, error) {
	_, done, err := s.begin()
	if err != nil {
		return "", err
	}
	defer done()
	if s.ready == nil || s.ready.id != id {
		return "", fmt.Errorf("预览已失效，请重新生成后打印")
	}
	p := s.ready
	return fmt.Sprintf("照片：%s\n打印机：%s · %s\n相纸：%s\n\n将按当前预览提交 1 张照片，消耗 1 张相纸和相应墨量。\n提交失败后请先检查打印机和出纸情况，避免重复出片。", p.preview.Name, p.request.Model, p.request.Host, p.preview.Paper.Name), nil
}

func (s *Service) Query(kind, host, model string) (Result, error) {
	ctx, done, err := s.begin()
	if err != nil {
		return Result{}, err
	}
	defer done()
	args := []string{kind, "--home", s.home}
	switch kind {
	case "discover":
		args = append(args, "--duration", "3s")
	case "status", "doctor":
		if err = validateTarget(host, model); err != nil {
			return Result{}, err
		}
		args = append(args, "--host", host, "--model", model)
	default:
		return Result{}, fmt.Errorf("不支持的查询")
	}
	return s.execute(ctx, args, nil)
}

func (s *Service) Calibrate(r Request, x, y float64) error {
	ctx, done, err := s.begin()
	if err != nil {
		return err
	}
	defer done()
	if err = validateTarget(r.Host, r.Model); err != nil {
		return err
	}
	if _, err = photo.GetProfile(r.Model, r.Paper); err != nil {
		return err
	}
	_, err = s.execute(ctx, []string{"calibrate", "set", "--home", s.home, "--host", r.Host, "--model", r.Model, "--paper", r.Paper, "--offset-x-mm", fmt.Sprint(x), "--offset-y-mm", fmt.Sprint(y)}, nil)
	if err == nil {
		s.invalidate()
	}
	return err
}

func (s *Service) Export(id, path string) error {
	_, done, err := s.begin()
	if err != nil {
		return err
	}
	defer done()
	if s.ready == nil || s.ready.id != id {
		return fmt.Errorf("请先重新生成预览")
	}
	b, err := os.ReadFile(filepath.Join(s.ready.dir, "full.jpg"))
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("无法导出（请选择一个新文件名）：%w", err)
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	return closeErr
}
