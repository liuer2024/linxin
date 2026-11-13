package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"log"
	"mac2win/internal/core"
	"mac2win/internal/lan"
	"mac2win/internal/preferences"
	"mac2win/internal/tailscale"
	"os"
	"path/filepath"
	"time"
)

type conversationUI struct {
	sending  bool
	input    string
	list     ui.ListState
	previous []core.Message
	reading  bool
	attempts map[string]bool
}
type app struct {
	win                                    *mygo.Window
	service                                *lan.Service
	page, source                           int
	selected, search, status, address, pin string
	busy, details, manualOpen              bool
	sessions                               map[string]*conversationUI
	opened                                 map[string]time.Time
	peerList                               ui.ListState
	dataDir                                string
	settings, settingsDraft                preferences.Settings
	settingsOpen, settingsSaving           bool
	settingsError                          string
	tailClient                             tailscale.Client
	tailState                              tailscale.Status
	tailLoading                            bool
	tailError, tailPort                    string
	tailUpdated                            time.Time
	tailCancel, tailConnectCancel          context.CancelFunc
	tailGeneration                         int
	cancels                                map[string]context.CancelFunc
	images                                 map[string]*imageEntry
}

func (a *app) session(id string) *conversationUI {
	if s := a.sessions[id]; s != nil {
		return s
	}
	s := &conversationUI{attempts: map[string]bool{}}
	a.sessions[id] = s
	return s
}
func (a *app) selectPeer(id string) {
	a.selected = id
	a.opened[id] = time.Now()
	a.session(id)
	a.page = 0
	a.details = false
}
func (a *app) work(label string, fn func(context.Context) error) {
	if a.busy {
		return
	}
	a.busy = true
	a.status = label
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := fn(ctx)
		a.win.Update(func() {
			a.busy = false
			a.status = ""
			if err != nil {
				a.status = err.Error()
			}
		})
	}()
}
func (a *app) view(c *ui.Context) {
	theme := *c.Theme()
	theme.Accent = ui.Oklch(.49, .15, 330)
	theme.AccentHover = ui.Oklch(.44, .15, 330)
	theme.AccentPressed = ui.Oklch(.39, .13, 330)
	theme.AccentText = ui.Oklch(1, 0, 0)
	theme.Focus = theme.Accent
	if theme.Dark {
		theme.TextMuted = ui.Oklch(.74, .005, 330)
	} else {
		theme.TextMuted = ui.Oklch(.48, .005, 330)
	}
	c.SetTheme(&theme)
	ui.Row(c).Fill().Children(func() {
		a.navigation(c)
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
			if a.service == nil {
				ui.Text(c, "服务启动失败，请检查网络或重启应用。").Padding(32).Grow(1)
			} else {
				switch a.page {
				case 0:
					a.messagesPage(c)
				case 1:
					a.discoverPage(c)
				case 2:
					a.transfersPage(c)
				}
			}
			if a.status != "" {
				ui.Row(c).Padding(10, 20).Gap(12).Background(c.Theme().Surface).Children(func() {
					ui.Text(c, a.status).FontSize(12).MaxLines(3).Grow(1)
					if ui.Button(c, "关闭提示").Clicked() {
						a.status = ""
					}
				})
			}
		})
	})
	a.settingsView(c)
	a.manualView(c)
}
func (a *app) navigation(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Width(76).Shrink(0).FillHeight().Padding(14, 8).Gap(16).Background(t.Surface).Children(func() {
		ui.Box(c).FillWidth().Height(50).Center().Children(func() {
			if b := brandBitmap(a.settings.AppIcon); b != nil {
				ui.Image(c, b).Size(32, 32).Fit(ui.Contain)
			}
		})
		unread := 0
		if a.service != nil {
			for _, s := range a.service.Store.Conversations() {
				unread += s.Unread
			}
		}
		for i, item := range []struct{ label, icon string }{{"消息", "messages"}, {"发现", "discover"}, {"传输", "transfer"}} {
			item, i := item, i
			label := item.label
			if i == 0 && unread > 0 {
				label += fmt.Sprintf(" %d", unread)
			}
			button := ui.ButtonBase(c.Key(item.icon)).Label(item.label).Height(68).Radius(10).Padding(8, 2)
			if a.page == i {
				button.Background(t.Background).TextColor(t.Accent)
			} else {
				button.TextColor(t.TextMuted)
			}
			if button.Hovered() {
				button.Background(t.SurfaceHover)
			}
			button.Children(func() {
				ui.Column(c).Center().Gap(5).Children(func() {
					ui.Icon(c, icons[item.icon]).Width(23).Height(23)
					ui.Text(c, label).FontSize(11).TextAlign(ui.Center)
				})
			})
			if button.Clicked() {
				a.page = i
				if i == 1 && a.source == 1 && a.tailUpdated.IsZero() {
					a.refreshTailscale()
				}
			}
		}
		ui.Spacer(c)
		button := ui.ButtonBase(c).Label("设置").Height(60).Radius(10).TextColor(t.TextMuted)
		button.Children(func() {
			ui.Column(c).Center().Gap(5).Children(func() { ui.Icon(c, icons["settings"]).Width(22).Height(22); ui.Text(c, "设置").FontSize(11) })
		})
		if button.Clicked() {
			a.openSettings()
		}
	})
}
func (a *app) chooseFiles(peer string) {
	a.work("选择并校验文件…", func(ctx context.Context) error {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "发送文件", Multiple: true})
		if err != nil {
			return err
		}
		for _, path := range paths {
			if err = a.service.Offer(ctx, peer, path); err != nil {
				return err
			}
		}
		return nil
	})
}
func (a *app) receive(f lan.Transfer) {
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, DefaultPath: f.Name, Title: "接收文件（请选择新的文件名）"})
		if err != nil {
			a.win.Update(func() { a.status = err.Error() })
			return
		}
		if path == "" {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a.win.Update(func() { a.cancels[f.ID] = cancel })
		err = a.service.Accept(ctx, f.Peer, f.ID, path)
		a.win.Update(func() {
			delete(a.cancels, f.ID)
			if err != nil {
				a.status = err.Error()
			} else {
				a.status = "文件已保存：" + path
			}
		})
	}()
}
func size(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	if n < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
}
func main() {
	config, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	dir := flag.String("data", filepath.Join(config, "lantern"), "device identity directory")
	listen := flag.String("listen", ":47832", "TLS listen address")
	name := flag.String("name", "", "device display name")
	tailCLI := flag.String("tailscale-cli", "", "Tailscale CLI executable (optional)")
	capture := flag.String("capture-settings", "", "capture this native settings window to a PNG (local QA)")
	flag.Parse()
	saved, settingsErr := preferences.Load(*dir)
	a := &app{page: 1, dataDir: *dir, settings: saved, tailPort: "47832", tailClient: tailscale.Client{Command: *tailCLI}, sessions: map[string]*conversationUI{}, opened: map[string]time.Time{}, cancels: map[string]context.CancelFunc{}, images: map[string]*imageEntry{}}
	mygo.App.WhenReady(func() {
		title := "邻信"
		if *name != "" {
			title += " · " + *name
		}
		a.win = mygo.NewWindow(mygo.WindowOptions{Title: title, Width: 1180, Height: 780, Content: ui.View(a.view)})
		if iconErr := a.applyAppIcon(); iconErr != nil {
			a.status = iconErr.Error()
		}
		a.service, err = lan.Start(*dir, *name, *listen, func() { a.win.Update(func() {}) })
		if err != nil {
			a.status = "启动失败：" + err.Error()
			a.win.Invalidate()
			return
		}
		_ = a.service.Discover()
		if settingsErr != nil {
			a.status = "读取设置失败：" + settingsErr.Error()
		}
		if a.settings.TailscaleEnabled && a.settings.StartTailscale {
			a.launchTailscale()
		}
		a.win.OnFocus(func() { a.win.Invalidate() })
		a.win.Invalidate()
		if *capture != "" {
			a.captureSettings(*capture)
		}
	})
	if err = mygo.App.Run(); err != nil {
		log.Print(err)
	}
	if a.service != nil {
		_ = a.service.Close()
	}
}
