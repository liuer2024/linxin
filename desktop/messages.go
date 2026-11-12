package main

import (
	"context"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"mac2win/internal/core"
	"mac2win/internal/lan"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type recent struct {
	peer     lan.Peer
	last     core.Message
	unread   int
	activity time.Time
}

func (a *app) recents() []recent {
	peers := map[string]lan.Peer{}
	for _, p := range a.service.Peers() {
		peers[p.ID] = p
	}
	rows := map[string]recent{}
	for id, opened := range a.opened {
		if p, ok := peers[id]; ok {
			rows[id] = recent{peer: p, activity: opened}
		}
	}
	for _, s := range a.service.Store.Conversations() {
		p, ok := peers[s.Peer]
		if !ok {
			continue
		}
		rows[s.Peer] = recent{peer: p, last: s.Last, unread: s.Unread, activity: s.Last.Created}
	}
	out := []recent{}
	query := strings.ToLower(strings.TrimSpace(a.search))
	for _, r := range rows {
		if query == "" || strings.Contains(strings.ToLower(r.peer.Name), query) || strings.Contains(strings.ToLower(r.last.Text), query) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].activity.Equal(out[j].activity) {
			return out[i].peer.ID < out[j].peer.ID
		}
		return out[i].activity.After(out[j].activity)
	})
	return out
}
func avatar(c *ui.Context, p lan.Peer, side float32) {
	t := c.Theme()
	fill := ui.Oklch(.93, .025, 330)
	if t.Dark {
		fill = ui.Oklch(.28, .025, 330)
	}
	letter, _ := utf8.DecodeRuneInString(p.Name)
	if letter == utf8.RuneError || letter == 0 {
		letter = '邻'
	}
	ui.Box(c).Width(side).Height(side).Shrink(0).Radius(side * .28).Background(fill).Center().Children(func() { ui.Text(c, string(letter)).FontSize(side * .4).Bold().TextColor(t.Accent) })
}
func (a *app) messagesPage(c *ui.Context) {
	t := c.Theme()
	narrow := a.win.Bounds().Width < 820
	ui.Row(c).Fill().Children(func() {
		if !narrow || a.selected == "" {
			panel := ui.Column(c).Width(288).Shrink(0).FillHeight().BorderWidth(0, 1, 0, 0).BorderColor(t.Border)
			if narrow {
				panel.Grow(1)
			}
			panel.Children(func() {
				ui.Column(c).Padding(22, 18, 14).Gap(14).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "消息").FontSize(22).Bold().Grow(1)
						if ui.Button(c, "新会话").Clicked() {
							a.page = 1
						}
					})
					ui.TextInput(c, &a.search).Label("搜索会话").Placeholder("搜索联系人或消息")
				})
				rows := a.recents()
				a.peerList.Key = func(i int) any { return rows[i].peer.ID }
				ui.List(c.Key("recents"), &a.peerList, len(rows), func(i int) {
					r := rows[i]
					p := r.peer
					row := ui.ButtonBase(c.Key(p.ID)).Label("会话 "+p.Name).Padding(12, 16).MinHeight(82).FillWidth()
					if p.ID == a.selected {
						row.Background(t.Surface)
					} else if row.Hovered() {
						row.Background(t.SurfaceHover)
					}
					row.Children(func() {
						ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
							avatar(c, p, 42)
							ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
								ui.Row(c).Gap(6).Children(func() {
									ui.Text(c, p.Name).Bold().MaxLines(1).Ellipsis("…").Grow(1)
									if !r.last.Created.IsZero() {
										ui.Text(c, r.last.Created.Format("15:04")).FontSize(10).TextColor(t.TextMuted)
									}
								})
								preview := r.last.Text
								if r.last.FileID != "" {
									preview = "[文件] " + preview
								}
								if preview == "" {
									preview = "开始聊天"
								}
								if s := a.sessions[p.ID]; s != nil && strings.TrimSpace(s.input) != "" {
									preview = "[草稿] " + s.input
								}
								ui.Row(c).Gap(6).Children(func() {
									ui.Text(c, preview).FontSize(12).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…").Grow(1)
									if r.unread > 0 {
										ui.Text(c, fmt.Sprint(r.unread)).FontSize(10).TextColor(t.AccentText).Background(t.Accent).Radius(9).Padding(2, 5)
									}
								})
							})
						})
					})
					if row.Clicked() {
						a.selectPeer(p.ID)
					}
				}).Grow(1).Children(func() {
					if len(rows) == 0 {
						ui.Column(c).Padding(24, 18).Gap(10).Children(func() {
							ui.Text(c, "还没有会话").Bold()
							ui.Text(c, "去发现页找到电脑，或等待对方发来消息。").FontSize(12).TextColor(t.TextMuted)
							if ui.Button(c, "发现设备").Clicked() {
								a.page = 1
							}
						})
					}
				})
			})
		}
		if !narrow || a.selected != "" {
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				if a.selected == "" {
					ui.Column(c).Fill().Center().Gap(16).Children(func() {
						ui.Icon(c, icons["messages"]).Width(42).Height(42).TextColor(t.TextMuted)
						ui.Text(c, "选择一个会话").FontSize(20).Bold()
						ui.Text(c, "在不同会话之间，轻松切换聊天与文件。").FontSize(13).TextColor(t.TextMuted)
					})
				} else {
					a.conversation(c)
				}
			})
		}
	})
}
func (a *app) conversation(c *ui.Context) {
	t := c.Theme()
	peer := a.selected
	p, err := a.service.Peer(peer)
	if err != nil {
		ui.Text(c, "设备暂不可用").Padding(24)
		return
	}
	s := a.session(peer)
	ui.Row(c).Padding(17, 22).Gap(12).AlignItems(ui.Center).Children(func() {
		if a.win.Bounds().Width < 820 {
			if ui.Button(c, "返回会话").Clicked() {
				a.selected = ""
			}
		}
		ui.Column(c).Gap(4).Grow(1).Children(func() {
			ui.Text(c, p.Name).FontSize(19).Bold()
			state := "在线"
			if !p.Online() {
				state = "连接待确认"
			}
			ui.Text(c, state).FontSize(11).TextColor(t.TextMuted)
		})
		if ui.Button(c, "详情").Clicked() {
			a.details = !a.details
		}
	})
	ui.Divider(c)
	ui.Row(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() { a.messageTimeline(c, p, s); a.composer(c, p, s) })
		if a.details {
			a.detailPanel(c, p)
		}
	})
}
func (a *app) messageTimeline(c *ui.Context, p lan.Peer, s *conversationUI) {
	t := c.Theme()
	peer := p.ID
	messages := a.service.Store.Messages(peer)
	files := map[string]lan.Transfer{}
	for _, f := range a.service.Transfers(peer) {
		files[f.ID] = f
	}
	first, last := s.list.Visible()
	ids := []string{}
	if a.win.IsFocused() && !a.settingsOpen && !a.manualOpen {
		for i := max(0, first); i <= last && i < len(s.previous); i++ {
			m := s.previous[i]
			if m.Incoming && m.State != core.Read && !s.attempts[m.ID] {
				ids = append(ids, m.ID)
			}
		}
	}
	if len(ids) > 0 && !s.reading {
		if len(ids) > 1000 {
			ids = ids[:1000]
		}
		s.reading = true
		for _, id := range ids {
			s.attempts[id] = true
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := a.service.ReadVisible(ctx, peer, ids)
			a.win.Update(func() {
				s.reading = false
				if err != nil {
					a.status = "已读回执暂未送达，可在会话详情中重试"
				}
			})
		}()
	}
	s.list.Key = func(i int) any { return messages[i].ID }
	s.list.FollowEnd = true
	background := ui.Oklch(.975, 0, 0)
	if t.Dark {
		background = ui.Oklch(.17, 0, 0)
	}
	ui.List(c.Key("timeline-"+peer), &s.list, len(messages), func(i int) {
		m := messages[i]
		ui.Column(c.Key(m.ID)).Gap(6).Padding(7, 0).Children(func() {
			if i == 0 || m.Created.Sub(messages[i-1].Created) > 5*time.Minute {
				ui.Text(c, m.Created.Format("15:04")).FontSize(10).TextColor(t.TextMuted).TextAlign(ui.Center).FillWidth().Margin(0, 0, 0, 8)
			}
			ui.Row(c).Gap(9).AlignItems(ui.Start).Children(func() {
				if !m.Incoming {
					ui.Spacer(c)
				} else {
					avatar(c, p, 32)
				}
				bubble := ui.Column(c).MaxWidth(420).MinWidth(56).Gap(7).Padding(11, 14).Radius(10)
				if m.Incoming {
					bubble.Background(t.Background)
				} else {
					bubble.Background(t.Accent).TextColor(t.AccentText)
				}
				bubble.Children(func() {
					if m.FileID == "" {
						ui.Text(c, m.Text).Selectable().MaxWidth(390)
					} else if f, ok := files[m.FileID]; ok {
						a.fileContent(c, f, true)
					}
				})
				if m.Incoming {
					ui.Spacer(c)
				}
			})
			if !m.Incoming {
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					label := map[core.State]string{core.Pending: "发送中", core.Failed: "发送失败", core.Delivered: "已送达", core.Read: "已读"}[m.State]
					ui.Text(c, label).FontSize(10).TextColor(t.TextMuted)
					if m.State == core.Failed && m.FileID == "" {
						if ui.Button(c, "重试").Disabled(a.busy).Clicked() {
							a.work("重试发送…", func(ctx context.Context) error { return a.service.Retry(ctx, peer, m.ID) })
						}
					}
				})
			}
		})
	}).Grow(1).MinHeight(80).Padding(16, 22).Gap(7).Background(background).Children(func() {
		if len(messages) == 0 {
			ui.Text(c, "打个招呼，或发送一个文件。").FontSize(13).TextColor(t.TextMuted).Margin(ui.Auto)
		}
	})
	s.previous = messages
}
func (a *app) composer(c *ui.Context, p lan.Peer, s *conversationUI) {
	t := c.Theme()
	ui.Column(c).Padding(12, 20).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			if ui.Button(c, "发送文件").Disabled(a.busy).Clicked() {
				a.chooseFiles(p.ID)
			}
			if ui.Button(c, "截图").Disabled(a.busy).Clicked() {
				a.captureScreenshot(p.ID)
			}
			ui.Spacer(c)
			ui.Text(c, "消息直接发送到对方电脑 · 粘贴图片或文件即发送").FontSize(10).TextColor(t.TextMuted)
		})
		area := ui.TextArea(c.Key("draft-"+p.ID), &s.input).Label("给 " + p.Name + " 的消息").Placeholder("输入消息，或粘贴截图…").Height(74).FillWidth().
			OnPaste(func(text string) bool { return text == "" && a.pasteIntoConversation(p.ID) })
		opts := transfer.DropOptions{Formats: []transfer.Format{transfer.FileList}, Operations: transfer.Copy}
		if drop, ok := ui.DropData(area, opts); ok && !a.busy {
			paths, err := drop.Data.Files()
			if err != nil {
				a.status = err.Error()
			} else {
				a.work("准备文件…", func(ctx context.Context) error {
					for _, path := range paths {
						if err := a.service.Offer(ctx, p.ID, path); err != nil {
							return err
						}
					}
					return nil
				})
			}
		}
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, "⌘ / Ctrl + Enter 发送").FontSize(10).TextColor(t.TextMuted)
			ui.Spacer(c)
			send := ui.PrimaryButton(c, "发送").Disabled(s.sending || strings.TrimSpace(s.input) == "").Clicked()
			shortcut := area.FocusWithin() && (c.Shortcut(ui.Cmd, ui.KeyEnter) || c.Shortcut(ui.Ctrl, ui.KeyEnter))
			if (send || shortcut) && !s.sending && strings.TrimSpace(s.input) != "" {
				text := s.input
				s.input = ""
				a.sendMessage(p.ID, text, s)
			}
		})
	})
}
func (a *app) detailPanel(c *ui.Context, p lan.Peer) {
	t := c.Theme()
	width := 250.0
	if a.win.Bounds().Width < 1050 {
		width = 210
	}
	ui.Column(c).Width(float32(width)).Shrink(0).FillHeight().Padding(18).Gap(12).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).Children(func() {
		ui.Text(c, "会话详情").FontSize(16).Bold()
		avatar(c, p, 46)
		ui.Text(c, p.Name).Bold()
		network := "局域网 / 手动"
		if p.Network == "tailscale" {
			network = "Tailscale"
		}
		ui.Text(c, "加密连接 · "+network).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, connectionHost(p.Address)).FontSize(11).Selectable().TextColor(t.TextMuted)
		ui.Divider(c)
		state := "身份未验证"
		if p.Verified {
			state = "已核对身份"
		}
		ui.Text(c, state).Bold()
		ui.Text(c, p.ID).FontSize(10).Selectable()
		if ui.Button(c, "复制对方指纹").Clicked() {
			mygo.Clipboard.WriteText(p.ID)
		}
		if !p.Verified {
			if ui.Button(c, "我已核对指纹").Clicked() {
				if err := a.service.VerifyPeer(p.ID); err != nil {
					a.status = err.Error()
				}
			}
		}
		if ui.Button(c, "重试已读回执").Clicked() {
			a.session(p.ID).attempts = map[string]bool{}
		}
		ui.Spacer(c)
		if ui.Button(c, "收起详情").Clicked() {
			a.details = false
		}
	})
}

func (a *app) sendMessage(peer, text string, s *conversationUI) {
	s.sending = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := a.service.Send(ctx, peer, text)
		a.win.Update(func() {
			s.sending = false
			if err != nil {
				a.status = err.Error()
			}
		})
	}()
}
