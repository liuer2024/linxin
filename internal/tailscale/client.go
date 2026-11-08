// Package tailscale reads the installed Tailscale client's local status.
// It never changes VPN settings, logs in, or embeds a second VPN stack.
package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrNotInstalled = errors.New("未找到 Tailscale 客户端，请先安装并登录，再点击刷新")

const maxStatusBytes = 4 << 20

type Device struct {
	ID      string
	Name    string
	DNSName string
	IPs     []string
	Online  bool
	OS      string
}

func (d Device) Address(port int) (string, error) {
	if port < 1 || port > 65535 {
		return "", errors.New("端口应为 1–65535")
	}
	for _, ip := range d.IPs {
		if addr, err := netip.ParseAddr(ip); err == nil && isTailIP(addr) {
			return net.JoinHostPort(addr.String(), strconv.Itoa(port)), nil
		}
	}
	return "", errors.New("此设备没有可用的 Tailscale 地址")
}

type Status struct {
	BackendState string
	Self         Device
	Devices      []Device
	Health       []string
}

func (s Status) Ready() bool { return s.BackendState == "Running" && len(s.Self.IPs) > 0 }
func (s Status) Summary() string {
	switch s.BackendState {
	case "Running":
		if len(s.Self.IPs) == 0 {
			return "Tailscale 正在连接，尚未分配地址"
		}
		return "已连接 Tailscale · " + s.Self.IPs[0]
	case "NeedsLogin", "NoState":
		return "请在 Tailscale 客户端登录，再刷新设备列表"
	case "Stopped":
		return "Tailscale 已暂停，请先在客户端连接"
	case "NeedsMachineAuth":
		return "此设备等待 Tailscale 管理员批准"
	case "Starting":
		return "Tailscale 正在启动，请稍后刷新"
	default:
		return "Tailscale 尚未就绪（" + s.BackendState + "）"
	}
}

type rawDevice struct {
	ID           string
	HostName     string
	DNSName      string
	TailscaleIPs []string
	Online       bool
	OS           string
}

func decodeDevice(raw rawDevice, fallback string) Device {
	d := Device{ID: raw.ID, Name: raw.HostName, DNSName: strings.TrimSuffix(raw.DNSName, "."), Online: raw.Online, OS: raw.OS}
	if d.ID == "" {
		d.ID = fallback
	}
	if len(d.ID) > 512 {
		d.ID = d.ID[:512]
	}
	if d.Name == "" {
		d.Name = d.DNSName
	}
	if d.Name == "" {
		d.Name = "未命名设备"
	}
	if len(d.Name) > 128 {
		d.Name = d.Name[:128]
	}
	// Prefer IPv4; use native Tailscale IPv6 when IPv4 is unavailable. Ignore
	// subnet routes and arbitrary addresses in CLI output.
	for _, ip := range raw.TailscaleIPs {
		a, e := netip.ParseAddr(ip)
		if e == nil && isTailIP(a) && a.Is4() {
			d.IPs = append(d.IPs, a.String())
		}
	}
	for _, ip := range raw.TailscaleIPs {
		a, e := netip.ParseAddr(ip)
		if e == nil && isTailIP(a) && a.Is6() {
			d.IPs = append(d.IPs, a.String())
		}
	}
	return d
}

var tail4 = netip.MustParsePrefix("100.64.0.0/10")
var tail6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

func isTailIP(a netip.Addr) bool { return tail4.Contains(a) || tail6.Contains(a) }
func ParseStatus(data []byte) (Status, error) {
	if len(data) > maxStatusBytes {
		return Status{}, errors.New("Tailscale 状态数据过大")
	}
	var raw struct {
		BackendState string
		Self         rawDevice
		Peer         map[string]rawDevice
		Health       []string
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return Status{}, errors.New("无法读取 Tailscale 状态，请更新客户端后重试")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Status{}, errors.New("Tailscale 返回了无效状态数据")
	}
	if raw.BackendState == "" {
		return Status{}, errors.New("Tailscale 状态缺少 BackendState")
	}
	out := Status{BackendState: raw.BackendState, Self: decodeDevice(raw.Self, "self"), Health: raw.Health}
	seen := map[string]bool{}
	for key, p := range raw.Peer {
		d := decodeDevice(p, key)
		if len(d.IPs) == 0 || d.ID == out.Self.ID || seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		out.Devices = append(out.Devices, d)
	}
	sort.Slice(out.Devices, func(i, j int) bool {
		a, b := out.Devices[i], out.Devices[j]
		if a.Online != b.Online {
			return a.Online
		}
		if a.Name == b.Name {
			return a.ID < b.ID
		}
		return a.Name < b.Name
	})
	return out, nil
}

// Client.Command can explicitly select an installed CLI, e.g. a custom install
// location. Commands are invoked directly with fixed arguments, never a shell.
type Client struct{ Command string }

func FindCommand() (string, error) {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path, nil
	}
	paths := []string{}
	switch runtime.GOOS {
	case "darwin":
		paths = []string{"/Applications/Tailscale.app/Contents/MacOS/Tailscale", "/usr/local/bin/tailscale", "/opt/homebrew/bin/tailscale"}
	case "windows":
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if root != "" {
				paths = append(paths, filepath.Join(root, "Tailscale", "tailscale.exe"))
			}
		}
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", ErrNotInstalled
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Len() int      { return b.buffer.Len() }
func (b *boundedBuffer) Bytes() []byte { return b.buffer.Bytes() }
func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("command output limit exceeded")
	}
	return b.buffer.Write(p)
}
func (c Client) Status(ctx context.Context) (Status, error) {
	command := c.Command
	var err error
	if command == "" {
		command, err = FindCommand()
		if err != nil {
			return Status{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, "status", "--json")
	configureCommand(cmd)
	cmd.WaitDelay = time.Second
	stdout := &boundedBuffer{limit: maxStatusBytes}
	stderr := &boundedBuffer{limit: 8192}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		return Status{}, fmt.Errorf("读取 Tailscale 状态超时或已取消：%w", ctx.Err())
	}
	// NeedsLogin may return useful JSON alongside a non-zero exit code.
	if stdout.Len() > 0 {
		if status, parseErr := ParseStatus(stdout.Bytes()); parseErr == nil {
			if err == nil || !status.Ready() {
				return status, nil
			}
		} else if err == nil {
			return Status{}, parseErr
		}
	}
	if err != nil {
		return Status{}, errors.New("无法访问 Tailscale 本地服务，请确认客户端已启动并允许当前用户读取状态")
	}
	return Status{}, errors.New("Tailscale 没有返回有效状态")
}
