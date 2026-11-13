package main

import (
	"embed"
	"fmt"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

//go:embed assets/icon-*.png
var brandAssets embed.FS

var brandBitmaps = map[string]*ui.Bitmap{}

func iconData(style string) []byte {
	if style != "flow" {
		style = "minimal"
	}
	b, _ := brandAssets.ReadFile("assets/icon-" + style + ".png")
	return b
}

func brandBitmap(style string) *ui.Bitmap {
	if b := brandBitmaps[style]; b != nil {
		return b
	}
	b, err := ui.DecodeBitmap(iconData(style))
	if err != nil {
		return nil
	}
	brandBitmaps[style] = b
	return b
}

func (a *app) applyAppIcon() error {
	data := iconData(a.settings.AppIcon)
	if runtime.GOOS == "darwin" {
		style := a.settings.AppIcon
		if style != "flow" {
			style = "minimal"
		}
		data, _ = brandAssets.ReadFile("assets/icon-dock-" + style + ".png")
		if err := mygo.App.Dock.SetIcon(data); err != nil {
			return fmt.Errorf("更新应用图标失败：%w", err)
		}
	} else if err := a.win.SetIcon(data); err != nil {
		return fmt.Errorf("更新应用图标失败：%w", err)
	}
	return nil
}

func (a *app) iconSettings(c *ui.Context) {
	ui.Text(c, "应用图标").Bold()
	ui.Row(c).Gap(20).Children(func() {
		for _, option := range []struct{ id, name string }{{"minimal", "极简平面"}, {"flow", "灵动线条"}} {
			ui.Column(c).Gap(6).Children(func() {
				if b := brandBitmap(option.id); b != nil {
					ui.Image(c, b).Size(64, 64).Fit(ui.Contain)
				}
				label := option.name
				if a.settingsDraft.AppIcon == option.id {
					label += " · 已选"
				}
				if ui.Button(c, label).Disabled(a.settingsSaving).Clicked() {
					a.settingsDraft.AppIcon = option.id
				}
			})
		}
	})
	ui.Text(c, "保存后更新导航栏与运行中的应用图标，下次启动沿用。").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Divider(c)
}
