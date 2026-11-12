package lan

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/net/ipv4"
	"net"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const discoveryPort = 47831

var discoveryGroup = net.IPv4(239, 255, 87, 15)

type DiscoveryState struct {
	Interfaces int
	Error      string
	SendError  string
	ProbeError string
	Firewall   string
	Addresses  []string
	Updated    time.Time
}

func (s *Service) DiscoveryStatus() DiscoveryState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.discoveryState
}
func (s *Service) Devices() []Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Peer, 0, len(s.devices))
	for _, p := range s.devices {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (p Peer) Online() bool { return time.Since(p.Seen) < 30*time.Second }
func (s *Service) RefreshDiscovery() {
	select {
	case s.discoveryRefresh <- struct{}{}:
	default:
	}
}

type discoveryJob struct {
	hello hello
	host  string
}
type discoveryLink struct {
	listener *net.UDPConn
	sender   *net.UDPConn
	iface    net.Interface
	ip       net.IP
}

func (l *discoveryLink) Close() { l.listener.Close(); l.sender.Close() }

// Discover joins every active multicast-capable IPv4 interface. Announcements
// trigger a pinned public-metadata probe, never a conversation or hello.
func (s *Service) Discover() error {
	s.discoveryOnce.Do(func() { go s.discoveryLoop() })
	return nil
}
func (s *Service) discoveryLoop() {
	jobs := make(chan discoveryJob, 64)
	var pending sync.Map
	replies := make(chan struct{}, 1)
	for i := 0; i < 4; i++ {
		go func() {
			for {
				select {
				case <-s.ctx.Done():
					return
				case job := <-jobs:
					func() {
						defer pending.Delete(job.hello.ID)
						ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
						defer cancel()
						if err := s.probeDiscovered(ctx, job.hello, job.host); err != nil {
							s.mu.Lock()
							s.discoveryState.ProbeError = err.Error()
							s.mu.Unlock()
							s.notify()
						}
					}()
				}
			}
		}()
	}
	links := map[int]*discoveryLink{}
	go func() {
		if hint := firewallHint(discoveryPort, s.port); hint != "" {
			s.mu.Lock()
			s.discoveryState.Firewall = hint
			s.mu.Unlock()
			s.notify()
		}
	}()
	defer func() {
		for _, l := range links {
			l.Close()
		}
	}()
	receive := func(conn *net.UDPConn) {
		b := make([]byte, 2048)
		for {
			n, addr, err := conn.ReadFromUDP(b)
			if err != nil {
				return
			}
			var h hello
			if json.Unmarshal(b[:n], &h) != nil || !validAnnouncement(h) || h.ID == s.Identity.ID {
				continue
			}
			if h.Query {
				select {
				case replies <- struct{}{}:
				default:
				}
			}
			host := addr.IP.String()
			// A loopback-only test instance is reachable on this machine only. The
			// datagram must have originated from one of this host's own addresses.
			if h.Loopback {
				if !isLocalIP(addr.IP) {
					continue
				}
				host = "127.0.0.1"
			}
			s.mu.RLock()
			known, ok := s.devices[h.ID]
			count := len(s.devices)
			s.mu.RUnlock()
			if (!ok && count >= 256) || (ok && time.Since(known.Seen) < 10*time.Second) {
				continue
			}
			if _, exists := pending.LoadOrStore(h.ID, true); exists {
				continue
			}
			select {
			case jobs <- discoveryJob{h, host}:
			default:
				pending.Delete(h.ID)
			}
		}
	}
	reconcile := func() {
		available := map[int]bool{}
		ifaces, err := net.Interfaces()
		lastError := ""
		if err != nil {
			lastError = err.Error()
		}
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagPointToPoint != 0 {
				continue
			}
			addresses, _ := iface.Addrs()
			var ip net.IP
			for _, a := range addresses {
				candidate, _, e := net.ParseCIDR(a.String())
				if e == nil && candidate.To4() != nil && !candidate.IsLoopback() {
					ip = candidate.To4()
					break
				}
			}
			if ip == nil {
				continue
			}
			available[iface.Index] = true
			if old := links[iface.Index]; old != nil {
				if old.ip.Equal(ip) {
					continue
				}
				old.Close()
				delete(links, iface.Index)
			}
			listener, e := net.ListenMulticastUDP("udp4", &iface, &net.UDPAddr{IP: discoveryGroup, Port: discoveryPort})
			if e != nil {
				lastError = e.Error()
				continue
			}
			sender, e := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
			if e != nil {
				listener.Close()
				lastError = e.Error()
				continue
			}
			packet := ipv4.NewPacketConn(sender)
			if e = packet.SetMulticastInterface(&iface); e != nil {
				listener.Close()
				sender.Close()
				lastError = e.Error()
				continue
			}
			_ = packet.SetMulticastLoopback(true)
			_ = packet.SetMulticastTTL(1)
			_ = listener.SetReadBuffer(64 << 10)
			l := &discoveryLink{listener: listener, sender: sender, iface: iface, ip: ip}
			links[iface.Index] = l
			go receive(listener)
		}
		for index, l := range links {
			if !available[index] {
				l.Close()
				delete(links, index)
			}
		}
		if len(links) == 0 && lastError == "" {
			lastError = "没有可用的局域网网卡，请检查 Wi-Fi 或有线网络"
		}
		if len(links) > 0 {
			lastError = ""
		}
		addresses := make([]string, 0, len(links))
		for _, l := range links {
			addresses = append(addresses, net.JoinHostPort(l.ip.String(), strconv.Itoa(s.port)))
		}
		sort.Strings(addresses)
		s.mu.Lock()
		s.discoveryState.Interfaces = len(links)
		s.discoveryState.Error = lastError
		s.discoveryState.Addresses = addresses
		s.discoveryState.Updated = time.Now()
		s.mu.Unlock()
	}
	send := func(query bool) {
		loopback := s.listener.Addr().(*net.TCPAddr).IP.IsLoopback()
		b, _ := json.Marshal(hello{Query: query, Protocol: "lantern/1", ID: s.Identity.ID, Name: s.Name, Port: s.port, OS: runtime.GOOS, Loopback: loopback})
		sendError := ""
		for _, l := range links {
			if _, err := l.sender.WriteToUDP(b, &net.UDPAddr{IP: discoveryGroup, Port: discoveryPort}); err != nil {
				sendError = describeSendError(err)
			}
		}
		s.mu.Lock()
		changed := s.discoveryState.SendError != sendError
		s.discoveryState.SendError = sendError
		s.mu.Unlock()
		if changed {
			s.notify()
		}
	}
	reconcile()
	send(true)
	s.notify()
	lastReply := time.Time{}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-replies:
			if time.Since(lastReply) > time.Second {
				send(false)
				lastReply = time.Now()
			}
		case <-s.discoveryRefresh:
			// Re-probe known announcements on the next heartbeat rather than requiring
			// manual IP entry. Existing sessions and their unread counts are preserved.
			s.mu.Lock()
			for id, p := range s.devices {
				p.Seen = time.Time{}
				s.devices[id] = p
			}
			s.mu.Unlock()
			reconcile()
			send(true)
			s.notify()
		case <-ticker.C:
			reconcile()
			send(false)
			s.notify()
		}
	}
}
func validAnnouncement(h hello) bool {
	if h.Protocol != "" && h.Protocol != "lantern/1" {
		return false
	}
	b, err := hex.DecodeString(h.ID)
	return err == nil && len(b) == 32 && h.Port > 0 && h.Port <= 65535 && len(h.Name) <= 128
}
func isLocalIP(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	addresses, _ := net.InterfaceAddrs()
	for _, a := range addresses {
		local, _, err := net.ParseCIDR(a.String())
		if err == nil && ip.Equal(local) {
			return true
		}
	}
	return false
}
func (s *Service) probeDiscovered(ctx context.Context, h hello, host string) error {
	info, err := s.Probe(ctx, net.JoinHostPort(host, strconv.Itoa(h.Port)), h.ID)
	if err != nil {
		return err
	}
	if info.ID != h.ID {
		return fmt.Errorf("discovered identity mismatch")
	}
	p := Peer{ID: info.ID, Name: info.Name, OS: info.OS, Network: "lan", Address: "https://" + net.JoinHostPort(host, strconv.Itoa(info.Port)), Seen: time.Now()}
	s.mu.Lock()
	if _, exists := s.devices[p.ID]; !exists && len(s.devices) >= 256 {
		s.mu.Unlock()
		return fmt.Errorf("发现设备数量超过上限")
	}
	p.Verified = s.trusted[p.ID]
	s.devices[p.ID] = p
	s.discoveryState.ProbeError = ""
	// Refresh a connected local route, without replacing a selected Tailscale
	// route just because a second network interface saw the same device.
	if current, ok := s.peers[p.ID]; ok && current.Network != "tailscale" {
		s.peers[p.ID] = p
	}
	s.mu.Unlock()
	s.notify()
	return nil
}
func (s *Service) ConnectDevice(ctx context.Context, id string) error {
	s.mu.RLock()
	p, ok := s.devices[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("设备尚未被发现")
	}
	u, err := url.Parse(p.Address)
	if err != nil {
		return err
	}
	return s.Connect(ctx, u.Host, id)
}

// describeSendError turns a multicast send failure into something a user can
// act on. macOS blocks local-network traffic from apps that have not been
// granted the "Local Network" permission, which surfaces as "no route to host".
func describeSendError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "no route to host") {
		if runtime.GOOS == "darwin" {
			return "系统未允许邻信使用本地网络。请在「系统设置 → 隐私与安全性 → 本地网络」中打开邻信（从终端启动时打开对应终端），然后重新探测"
		}
		return "发现包无法发出：当前网络不可达，请检查网卡和路由器设置"
	}
	if strings.Contains(msg, "permission denied") {
		return "系统拒绝了邻信发送发现包，请检查本地网络权限或安全软件"
	}
	return "发现包发送失败：" + msg
}
