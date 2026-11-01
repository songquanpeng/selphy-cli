package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/songquanpeng/selphy-cli/internal/config"
	"github.com/songquanpeng/selphy-cli/internal/desktop"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx     context.Context
	service *desktop.Service
	err     error
	confirm sync.Mutex
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx; a.service, a.err = desktop.New(ctx, "") }
func (a *App) shutdown(_ context.Context) {
	if a.service != nil {
		a.service.Close()
	}
}
func (a *App) beforeClose(ctx context.Context) bool {
	if a.service == nil || !a.service.Busy() {
		return false
	}
	answer, _ := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{Type: runtime.WarningDialog, Title: "操作尚未结束", Message: "退出会请求取消当前操作。已进入打印的照片仍可能继续出纸。", Buttons: []string{"继续等待", "取消操作并退出"}, DefaultButton: "继续等待", CancelButton: "继续等待"})
	return answer != "取消操作并退出"
}

func (a *App) Boot() (config.Config, error) {
	if a.err != nil {
		return config.Config{}, a.err
	}
	return a.service.Config()
}
func (a *App) ChoosePhoto() (desktop.Selection, error) {
	if a.err != nil {
		return desktop.Selection{}, a.err
	}
	if a.service.Busy() {
		return desktop.Selection{}, fmt.Errorf("请等待当前操作结束")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择要打印的照片", Filters: []runtime.FileFilter{{DisplayName: "照片 (JPEG / PNG / WebP / HEIC)", Pattern: "*.jpg;*.jpeg;*.png;*.webp;*.heic;*.heif;*.JPG;*.JPEG;*.PNG;*.HEIC"}}})
	if err != nil || path == "" {
		return desktop.Selection{}, err
	}
	return a.service.Select(path)
}
func (a *App) Preview(r desktop.Request) (desktop.Preview, error) { return a.service.Prepare(r) }
func (a *App) SaveTarget(host, model string) error {
	return a.service.SaveTarget(strings.TrimSpace(host), model)
}
func (a *App) Query(kind, host, model string) (desktop.Result, error) {
	r, err := a.service.Query(kind, strings.TrimSpace(host), model)
	if kind == "doctor" && json.Valid(r.Data) {
		return r, nil
	}
	return r, err
}
func (a *App) Calibrate(r desktop.Request, x, y float64) error { return a.service.Calibrate(r, x, y) }
func (a *App) Cancel()                                         { a.service.Cancel() }

type progressWriter struct{ app *App }

func (w progressWriter) Write(b []byte) (int, error) {
	runtime.EventsEmit(w.app.ctx, "print-progress", string(b))
	return len(b), nil
}

func (a *App) Print(id string) (desktop.Result, error) {
	if !a.confirm.TryLock() {
		return desktop.Result{}, fmt.Errorf("打印确认已打开")
	}
	defer a.confirm.Unlock()
	message, err := a.service.Confirmation(id)
	if err != nil {
		return desktop.Result{}, err
	}
	answer, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "打印 1 张照片", Message: message, Buttons: []string{"取消", "打印 1 张"}, DefaultButton: "取消", CancelButton: "取消"})
	if err != nil {
		return desktop.Result{}, err
	}
	if answer != "打印 1 张" {
		return desktop.Result{Data: []byte(`{"state":"cancelled"}`)}, nil
	}
	return a.service.Print(id, progressWriter{a})
}

func (a *App) Export(id string) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "导出打印用 JPEG（不含标线）", DefaultFilename: "selphy-print.jpg", Filters: []runtime.FileFilter{{DisplayName: "JPEG 图片", Pattern: "*.jpg"}}})
	if err != nil || path == "" {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(path), ".jpg") && !strings.HasSuffix(strings.ToLower(path), ".jpeg") {
		path += ".jpg"
	}
	if err = a.service.Export(id, path); err != nil {
		return "", err
	}
	return path, nil
}
