package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend
var assets embed.FS
var version = "0.2.0"

func main() {
	app := &App{}
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	f := m.AddSubmenu("文件")
	f.AddText("选择照片…", nil, func(_ *menu.CallbackData) {
		if app.ctx != nil {
			runtime.EventsEmit(app.ctx, "choose-photo")
		}
	})
	m.Append(menu.EditMenu())
	err := wails.Run(&options.App{
		Title: "SELPHY Studio", Width: 1180, Height: 790, MinWidth: 980, MinHeight: 680,
		BackgroundColour: options.NewRGB(248, 247, 243),
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup, OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose,
		Bind: []interface{}{app}, Menu: m,
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "com.justsong.selphy-studio", OnSecondInstanceLaunch: func(_ options.SecondInstanceData) { runtime.WindowUnminimise(app.ctx); runtime.Show(app.ctx) }},
		Mac:                &mac.Options{TitleBar: mac.TitleBarHiddenInset(), Appearance: mac.NSAppearanceNameAqua, About: &mac.AboutInfo{Title: "SELPHY Studio " + version, Message: "本地照片打印工作台\n基于 selphy-cli · Go + Wails\n照片在本机处理，无需账户。"}},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
