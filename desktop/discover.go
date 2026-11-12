package main

import (
	"context"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"mac2win/internal/lan"
	"net/url"
	"strings"
)

func (a *app) discoverPage(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(28, 36).Gap(16).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(5).Children(func() {
				ui.Text(c, "发现").FontSize(24).Bold()
				ui.Text(c, "找到另一台电脑，把消息和文件送过去。").FontSize(13).TextColor(t.TextMuted)
			})
			if ui.Button(c, "手动连接").Clicked() {
				a.manualOpen = true
			}
		})
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			if a.settings.TailscaleEnabled {
				old := a.source
				ui.Segmented(c, &a.source, "附近设备", "Tailscale").Label("发现范围")
				if old != a.source && a.source == 1 && a.tailUpdated.IsZero() {
					a.refreshTailscale()
				}
			} else {
				ui.Text(c, "附近设备").Bold()
			}
			ui.Spacer(c)
			label := "重新探测"
			if a.source == 1 && a.tailLoading {
				label = "读取中…"
			}
			if ui.Button(c, label).Disabled(a.tailLoading && a.source == 1).Clicked() {
				if a.source == 1 {
					a.refreshTailscale()
				} else {
					a.service.RefreshDiscovery()
				}
			}
		})
		if a.source == 1 {
			a.tailscaleDiscovery(c)
			return
		}
		devices := []lan.Peer{}
		for _, p := range a.service.Devices() {
			if p.Network != "tailscale" && p.Online() {
				devices = append(devices, p)
			}
		}
		state := a.service.DiscoveryStatus()
		subtitle := fmt.Sprintf("自动发现已开启 · %d 台设备", len(devices))
		if len(devices) == 0 && state.ProbeError != "" {
			subtitle = "发现设备，但连接未成功：" + state.ProbeError
		}
		if state.Error != "" {
			subtitle = state.Error
		} else if state.SendError != "" {
			subtitle = state.SendError
		} else if state.Interfaces == 0 {
			subtitle = "正在检查本地网络…"
		}
		ui.Text(c, subtitle).FontSize(12).TextColor(t.TextMuted)
		if state.Firewall != "" && len(devices) == 0 {
			ui.Text(c, state.Firewall).FontSize(12).TextColor(t.TextMuted)
		}
		if len(devices) == 0 {
			ui.Column(c).Grow(1).Center().Gap(18).Children(func() {
				ui.Box(c).Width(112).Height(112).Radius(56).Background(t.Surface).Center().Children(func() { ui.Icon(c, icons["discover"]).Width(46).Height(46).TextColor(t.Accent) })
				ui.Text(c, "等待附近的电脑").FontSize(20).Bold()
				ui.Text(c, "两台电脑连接同一路由器并打开邻信，设备会自动出现在这里。").MaxWidth(440).TextAlign(ui.Center).TextColor(t.TextMuted)
				ui.Text(c, "如果仍未出现，请检查本地网络权限、访客网络隔离或防火墙。").FontSize(12).MaxWidth(440).TextAlign(ui.Center).TextColor(t.TextMuted)
			})
		} else {
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Row(c).Wrap().Justify(ui.Center).Gap(28).Padding(24, 0).Children(func() {
					for _, p := range devices {
						a.deviceTile(c, p)
					}
				})
			})
		}
		ui.Divider(c)
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Icon(c, icons["laptop"]).Width(22).Height(22).TextColor(t.TextMuted)
			local := "本机 · " + a.service.Name
			if len(state.Addresses) > 0 {
				local += " · " + strings.Join(state.Addresses, "  ")
			} else {
				local += fmt.Sprintf(" · 端口 %d", a.service.Port())
			}
			ui.Text(c, local).FontSize(12).Grow(1).MaxLines(1).Ellipsis("…")
			if len(state.Addresses) > 0 && ui.Button(c, "复制地址").Clicked() {
				mygo.Clipboard.WriteText(state.Addresses[0])
			}
			ui.Text(c, "对方可用「手动连接」填入本机地址").FontSize(12).TextColor(t.TextMuted)
		})
	})
}
func (a *app) deviceTile(c *ui.Context, p lan.Peer) {
	t := c.Theme()
	ui.Column(c.Key(p.ID)).Width(214).Padding(18, 8).Gap(9).AlignItems(ui.Center).Children(func() {
		avatar := ui.ButtonBase(c).Label("发送文件给 " + p.Name).Width(96).Height(96).Radius(48).Background(t.Surface).TextColor(t.Accent).Disabled(a.busy)
		if avatar.Hovered() {
			avatar.Background(t.SurfaceHover)
		}
		avatar.Children(func() {
			ui.Box(c).Fill().Center().Children(func() { ui.Icon(c, platformIcon(p.OS)).Width(38).Height(38) })
		})
		opts := transfer.DropOptions{Formats: []transfer.Format{transfer.FileList}, Operations: transfer.Copy}
		if _, over := ui.DataDragOver(avatar, opts); over {
			avatar.Background(t.Selection)
		}
		if drop, ok := ui.DropData(avatar, opts); ok && !a.busy {
			paths, err := drop.Data.Files()
			if err != nil {
				a.status = err.Error()
			} else if len(paths) > 0 {
				a.contactDevice(p, true, paths)
			}
		}
		if avatar.Clicked() {
			a.contactDevice(p, true, nil)
		}
		ui.Text(c, p.Name).Bold().MaxLines(1).Ellipsis("…").TextAlign(ui.Center).FillWidth()
		os := p.OS
		if os == "darwin" {
			os = "macOS"
		}
		if os == "" {
			os = "电脑"
		}
		ui.Text(c, os+" · 在线").FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Justify(ui.Center).Children(func() {
			if ui.Button(c, "聊天").Disabled(a.busy).Clicked() {
				a.contactDevice(p, false, nil)
			}
			if ui.Button(c, "发文件").Disabled(a.busy).Clicked() {
				a.contactDevice(p, true, nil)
			}
		})
	})
}
func (a *app) contactDevice(p lan.Peer, files bool, paths []string) {
	a.work("正在连接 "+p.Name+"…", func(ctx context.Context) error {
		if err := a.service.ConnectDevice(ctx, p.ID); err != nil {
			return err
		}
		if files {
			var err error
			if paths == nil {
				paths, err = mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "发送文件给 " + p.Name, Multiple: true})
				if err != nil {
					return err
				}
			}
			if len(paths) == 0 {
				return nil
			}
			for _, path := range paths {
				if err = a.service.Offer(ctx, p.ID, path); err != nil {
					return err
				}
			}
		}
		a.win.Update(func() { a.selectPeer(p.ID) })
		return nil
	})
}
func (a *app) manualView(c *ui.Context) {
	ui.Modal(c.Key("manual"), &a.manualOpen, func() {
		ui.Column(c).Width(420).Gap(14).Children(func() {
			ui.Text(c, "手动连接").FontSize(20).Bold()
			ui.Text(c, "自动发现不可用时，填写对方地址并核对设备指纹。").FontSize(12).TextColor(c.Theme().TextMuted)
			ui.TextInput(c, &a.address).Label("对方地址").Placeholder("IP 或主机名:端口")
			ui.TextInput(c, &a.pin).Label("对方指纹").Placeholder("完整的 64 位设备指纹")
			if ui.Button(c, "复制本机指纹").Clicked() {
				mygo.Clipboard.WriteText(a.service.Identity.ID)
			}
			ui.Text(c, fmt.Sprintf("本机连接端口 %d", a.service.Port())).FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					a.manualOpen = false
				}
				if ui.PrimaryButton(c, "连接").Disabled(a.busy || a.address == "" || len(a.pin) != 64).Clicked() {
					address, pin := a.address, a.pin
					a.manualOpen = false
					a.work("正在连接…", func(ctx context.Context) error {
						if err := a.service.Connect(ctx, address, pin); err != nil {
							return err
						}
						a.win.Update(func() { a.selectPeer(pin) })
						return nil
					})
				}
			})
		})
	}).Label("手动连接")
}
func (a *app) tailscaleDiscovery(c *ui.Context) {
	t := c.Theme()
	if a.tailError != "" {
		ui.Text(c, a.tailError).TextColor(t.TextMuted)
		if ui.Button(c, "获取 Tailscale").Clicked() {
			go func() { _ = mygo.Shell.OpenExternal("https://tailscale.com/download") }()
		}
		ui.Spacer(c)
		return
	}
	if a.tailLoading && a.tailUpdated.IsZero() {
		ui.Text(c, "正在读取 Tailscale 设备…")
		ui.Spacer(c)
		return
	}
	ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.tailState.Summary()).FontSize(12).TextColor(t.TextMuted).Grow(1)
		ui.Text(c, "对方端口").FontSize(12)
		ui.TextInput(c, &a.tailPort).Label("对方邻信端口").Width(90)
	})
	if !a.tailState.Ready() {
		ui.Spacer(c)
		return
	}
	ui.Text(c, "在线设备需要运行邻信，连接后才能聊天。").FontSize(12).TextColor(t.TextMuted)
	if len(a.tailState.Devices) == 0 {
		ui.Text(c, "还没有其他设备加入此网络。").Padding(24)
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Row(c).Wrap().Justify(ui.Center).Gap(28).Padding(24, 0).Children(func() {
			for _, d := range a.tailState.Devices {
				d := d
				ui.Column(c.Key(d.ID)).Width(214).Padding(18, 8).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Width(96).Height(96).Radius(48).Background(t.Surface).Center().Children(func() { ui.Icon(c, platformIcon(d.OS)).Width(38).Height(38).TextColor(t.Accent) })
					ui.Text(c, d.Name).Bold().MaxLines(1).Ellipsis("…")
					state := "离线"
					if d.Online {
						state = "Tailscale 在线"
					}
					ui.Text(c, state).FontSize(12).TextColor(t.TextMuted)
					if ui.Button(c, "连接并聊天").Disabled(!d.Online || a.busy).Clicked() {
						a.connectTailscale(d)
					}
				})
			}
		})
	})
}

// Used by the detail panel to display the selected transport without assuming
// that a Tailscale connection is direct rather than relayed.
func connectionHost(address string) string {
	u, e := url.Parse(address)
	if e != nil {
		return address
	}
	return u.Host
}
