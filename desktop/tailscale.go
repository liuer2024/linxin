package main

import (
	"context"
	"fmt"
	"mac2win/internal/tailscale"
	"strconv"
	"strings"
	"time"
)

func (a *app) refreshTailscale() {
	if !a.settings.TailscaleEnabled || a.tailLoading {
		return
	}
	a.tailLoading = true
	a.tailGeneration++
	generation := a.tailGeneration
	ctx, cancel := context.WithCancel(context.Background())
	a.tailCancel = cancel
	go func() {
		defer cancel()
		status, err := a.tailClient.Status(ctx)
		a.win.Update(func() {
			if generation != a.tailGeneration || !a.settings.TailscaleEnabled {
				return
			}
			a.tailCancel = nil
			a.tailLoading = false
			a.tailState = status
			a.tailUpdated = time.Now()
			a.tailError = ""
			if err != nil {
				a.tailError = err.Error()
			}
		})
	}()
}
func (a *app) connectTailscale(d tailscale.Device) {
	if !a.settings.TailscaleEnabled || a.busy {
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(a.tailPort))
	if err != nil || port < 1 || port > 65535 {
		a.status = "请输入 1–65535 之间的对端邻信端口"
		return
	}
	address, err := d.Address(port)
	if err != nil {
		a.status = err.Error()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.tailConnectCancel = cancel
	a.work("正在连接 "+d.Name+"…", func(workContext context.Context) error {
		defer cancel()
		stop := context.AfterFunc(workContext, cancel)
		defer stop()
		peer, err := a.service.ConnectTailscale(ctx, address, d.ID)
		if err != nil {
			return fmt.Errorf("Tailscale 设备连接失败：%w。请确认对方邻信已启动、端口正确，且访问策略和防火墙允许双向连接", err)
		}
		a.win.Update(func() {
			if a.settings.TailscaleEnabled {
				a.selectPeer(peer.ID)
			}
		})
		return nil
	})
}
