package main

import (
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"mac2win/internal/preferences"
	"mac2win/internal/tailscale"
	"os"
	"path/filepath"
	"runtime"
)

func (a *app) openSettings() {
	if a.settingsSaving {
		return
	}
	a.settingsDraft = a.settings
	a.settingsError = ""
	a.settingsOpen = true
}
func (a *app) settingsView(c *ui.Context) {
	ui.Modal(c.Key("settings"), &a.settingsOpen, func() {
		t := c.Theme()
		ui.Column(c).Width(440).MaxWidth(440).Gap(16).Children(func() {
			ui.Text(c, "设置").FontSize(22).Bold()
			a.iconSettings(c)
			ui.Text(c, "跨网络连接").Bold()
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, "启用 Tailscale").Bold()
					ui.Text(c, "显示跨网络设备，连接其他地点的电脑。").FontSize(12).TextColor(t.TextMuted)
				})
				ui.Switch(c, &a.settingsDraft.TailscaleEnabled).Label("启用 Tailscale").Disabled(a.settingsSaving)
			})
			ui.Divider(c)
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, "随邻信启动 Tailscale").Bold()
					ui.Text(c, "启动邻信时，打开已安装的 Tailscale 客户端。").FontSize(12).TextColor(t.TextMuted)
				})
				ui.Switch(c, &a.settingsDraft.StartTailscale).Label("随邻信启动 Tailscale").Disabled(!a.settingsDraft.TailscaleEnabled || a.settingsSaving)
			})
			ui.Text(c, "登录与 VPN 连接仍由 Tailscale 管理。关闭此处功能后，系统中的 Tailscale 可以继续运行。").FontSize(12).TextColor(t.TextMuted)
			if a.settingsError != "" {
				ui.Text(c, a.settingsError).FontSize(12)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Disabled(a.settingsSaving).Clicked() {
					a.settingsOpen = false
				}
				label := "保存"
				if a.settingsSaving {
					label = "保存中…"
				}
				if ui.PrimaryButton(c, label).Disabled(a.settingsSaving).Clicked() {
					a.saveSettings()
				}
			})
		})
	}).Label("设置")
}
func (a *app) saveSettings() {
	if a.settingsSaving {
		return
	}
	a.settingsSaving = true
	next := a.settingsDraft
	old := a.settings
	go func() {
		err := preferences.Save(a.dataDir, next)
		a.win.Update(func() {
			a.settingsSaving = false
			if err != nil {
				a.settingsError = "保存失败：" + err.Error()
				return
			}
			a.settings = next
			if err := a.applyAppIcon(); err != nil {
				a.status = err.Error()
			}
			a.settingsOpen = false
			if !next.TailscaleEnabled {
				a.source = 0
				a.tailGeneration++
				if a.tailCancel != nil {
					a.tailCancel()
					a.tailCancel = nil
				}
				if a.tailConnectCancel != nil {
					a.tailConnectCancel()
					a.tailConnectCancel = nil
				}
				a.tailLoading = false
				a.tailState = tailscale.Status{}
				a.tailError = ""
			} else if !old.TailscaleEnabled {
				a.refreshTailscale()
			}
			if next.TailscaleEnabled && next.StartTailscale && (!old.StartTailscale || !old.TailscaleEnabled) {
				a.launchTailscale()
			}
		})
	}()
}

// Launch the desktop client only. Never run `tailscale up/down`, install a
// service, or change OS startup/login items on behalf of this preference.
func (a *app) launchTailscale() {
	go func() {
		err := openTailscaleClient()
		if err != nil {
			a.win.Update(func() { a.status = err.Error() })
		}
	}()
}
func openTailscaleClient() error {
	paths := []string{}
	switch runtime.GOOS {
	case "darwin":
		paths = []string{"/Applications/Tailscale.app"}
		if home, e := os.UserHomeDir(); e == nil {
			paths = append(paths, filepath.Join(home, "Applications", "Tailscale.app"))
		}
	case "windows":
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if root != "" {
				paths = append(paths, filepath.Join(root, "Tailscale", "tailscale-ipn.exe"))
			}
		}
		if cli, e := tailscale.FindCommand(); e == nil {
			paths = append(paths, filepath.Join(filepath.Dir(cli), "tailscale-ipn.exe"))
		}
	default:
		return fmt.Errorf("当前平台请手动打开 Tailscale 客户端")
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return mygo.Shell.OpenPath(path)
		}
	}
	return fmt.Errorf("未找到 Tailscale 图形客户端。请先安装客户端，或手动启动已有的命令行服务")
}
