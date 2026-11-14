package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"time"
)

// Updates come from the GitHub releases of updates.github in mygo.json and
// must be signed with updates.publicKey. The native window needs no WebView.
// Development builds (go run) cannot update; the window says so on check.
func useUpdater() {
	mygo.Use(native.New(updater.Options{Language: "zh-Hans", Icon: iconData("minimal")}))
}

func appVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return "开发版"
}

func (a *app) updateSettings(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "软件更新").Bold()
	ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, "自动检查更新").Bold()
			ui.Text(c, "启动后在后台检查新版本，之后每天检查一次。").FontSize(12).TextColor(t.TextMuted)
		})
		ui.Switch(c, &a.autoUpdateDraft).Label("自动检查更新").Disabled(a.settingsSaving)
	})
	ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, "当前版本 "+appVersion()).FontSize(12)
			line := ""
			if v := updater.AvailableVersion(); v != "" {
				line = "新版本 " + v + " 可用"
			} else if last := updater.LastCheck(); !last.IsZero() {
				line = "上次检查：" + last.In(beijing).Format("2006-01-02 15:04")
			}
			if !mygo.Updater.Enabled() {
				line = "开发运行的版本不能自动更新，请使用打包后的应用。"
			}
			if line != "" {
				ui.Text(c, line).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if ui.Button(c, "检查更新").Disabled(a.settingsSaving).Clicked() {
			updater.CheckForUpdates()
		}
	})
	ui.Divider(c)
}

var beijing = time.FixedZone("CST", 8*3600)
