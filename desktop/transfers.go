package main

import (
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"mac2win/internal/lan"
	"sort"
)

func (a *app) transfersPage(c *ui.Context) {
	t := c.Theme()
	files := a.service.AllTransfers()
	sort.Slice(files, func(i, j int) bool { return files[i].Created.After(files[j].Created) })
	ui.Column(c).Fill().Padding(28, 36).Gap(18).Children(func() {
		ui.Text(c, "文件传输").FontSize(24).Bold()
		ui.Text(c, "所有会话的文件，都在这里。").FontSize(13).TextColor(t.TextMuted)
		ui.Divider(c)
		if len(files) == 0 {
			ui.Column(c).Grow(1).Center().Gap(16).Children(func() {
				ui.Icon(c, icons["transfer"]).Width(42).Height(42).TextColor(t.TextMuted)
				ui.Text(c, "还没有传输任务").FontSize(20).Bold()
				ui.Text(c, "从发现页发送文件，或在会话中接收文件。").TextColor(t.TextMuted)
				if ui.Button(c, "发现设备").Clicked() {
					a.page = 1
				}
			})
			return
		}
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				for _, f := range files {
					f := f
					ui.Row(c.Key(f.Peer+f.ID)).Padding(16, 0).Gap(16).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
						ui.Icon(c, icons["file"]).Width(28).Height(28).TextColor(t.Accent)
						ui.Column(c).Grow(1).Gap(7).Children(func() { a.fileContent(c, f, false) })
						if p, err := a.service.Peer(f.Peer); err == nil {
							if ui.Button(c, "查看会话 · "+p.Name).Clicked() {
								a.selectPeer(p.ID)
							}
						}
					})
				}
			})
		})
	})
}
func (a *app) fileContent(c *ui.Context, f lan.Transfer, compact bool) {
	if compact && f.Path != "" && isImageName(f.Name) && (!f.Incoming || f.State == "已完成") {
		a.imagePreview(c, f.Path)
	}
	ui.Text(c, f.Name).Bold().MaxLines(2).Ellipsis("…")
	ui.Text(c, fmt.Sprintf("%s · %s", size(f.Size), f.State)).FontSize(12)
	if f.State == "传输中" {
		ui.Text(c, fmt.Sprintf("%s / %s", size(f.Done), size(f.Size))).FontSize(11)
	}
	if f.Error != "" {
		ui.Text(c, f.Error).FontSize(11).MaxLines(3)
	}
	if f.Incoming && f.State == "等待接收" {
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "接收并另存为").Clicked() {
				a.receive(f)
			}
			if ui.Button(c, "拒绝").Clicked() {
				go a.service.Decline(f.Peer, f.ID)
			}
		})
	}
	if cancel := a.cancels[f.ID]; cancel != nil && f.State == "传输中" {
		if ui.Button(c, "取消接收").Clicked() {
			cancel()
		}
	}
	if f.State == "已完成" && f.Path != "" {
		ui.Row(c).Children(func() {
			if ui.Button(c, "打开所在文件夹").Clicked() {
				go mygo.Shell.ShowItemInFolder(f.Path)
			}
		})
	}
}
