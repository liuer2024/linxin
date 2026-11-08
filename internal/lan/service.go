// Package lan implements certificate-identified local-network sessions.
package lan

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mac2win/internal/core"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Peer struct {
	OS       string    `json:"os"`
	Network  string    `json:"network"`
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Address  string    `json:"address"`
	Seen     time.Time `json:"seen"`
	Verified bool      `json:"verified"`
}
type hello struct {
	OS       string `json:"os,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Loopback bool   `json:"loopback,omitempty"`
	Query    bool   `json:"query,omitempty"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Port     int    `json:"port"`
}
type Service struct {
	dir              string
	devices          map[string]Peer
	discoveryOnce    sync.Once
	discoveryState   DiscoveryState
	discoveryRefresh chan struct{}
	endpointPins     map[string]string
	trusted          map[string]bool
	Identity         Identity
	Name             string
	Store            *core.Store
	mu               sync.RWMutex
	peers            map[string]Peer
	files            map[string]*Transfer
	server           *http.Server
	listener         net.Listener
	port             int
	changed          func()
	ctx              context.Context
	cancel           context.CancelFunc
	clients          map[string]*http.Client
}

func Start(dir, name, listen string, onChange func()) (*Service, error) {
	pins, err := loadEndpointPins(dir)
	if err != nil {
		return nil, err
	}
	trusted, err := loadTrust(dir)
	if err != nil {
		return nil, err
	}
	identity, err := LoadIdentity(dir)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name, _ = os.Hostname()
	}
	if len(name) > 128 {
		name = name[:128]
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{devices: map[string]Peer{}, discoveryRefresh: make(chan struct{}, 1), endpointPins: pins, dir: dir, trusted: trusted, Identity: identity, Name: name, Store: core.NewStore(), peers: map[string]Peer{}, files: map[string]*Transfer{}, clients: map[string]*http.Client{}, listener: listener, port: listener.Addr().(*net.TCPAddr).Port, changed: onChange, ctx: ctx, cancel: cancel}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/hello", s.handleHello)
	mux.HandleFunc("/v1/identity", s.handleIdentity)
	node := &core.Node{Store: s.Store}
	mux.Handle("/v1/messages", s.observe(node.Handler()))
	mux.Handle("/v1/read", s.observe(node.Handler()))
	mux.HandleFunc("/v1/files/offer", s.handleOffer)
	mux.HandleFunc("/v1/files/content/", s.handleContent)
	mux.HandleFunc("/v1/files/status", s.handleFileStatus)
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{identity.Certificate}, ClientAuth: tls.RequireAnyClientCert}}
	go func() { _ = s.server.Serve(tls.NewListener(listener, s.server.TLSConfig)) }()
	return s, nil
}
func (s *Service) Close() error {
	s.cancel()
	s.mu.Lock()
	for _, c := range s.clients {
		c.CloseIdleConnections()
	}
	s.mu.Unlock()
	return s.server.Close()
}
func (s *Service) notify() {
	if s.changed != nil {
		s.changed()
	}
}
func (s *Service) Port() int { return s.port }
func (s *Service) Peers() []Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (s *Service) Peer(id string) (Peer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.peers[id]
	if !ok {
		return Peer{}, errors.New("设备尚未连接")
	}
	return p, nil
}
func (s *Service) client(id string) *http.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.clients[id]; c != nil {
		return c
	}
	c := core.PinnedClient(id)
	c.Transport.(*http.Transport).TLSClientConfig.Certificates = []tls.Certificate{s.Identity.Certificate}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s.clients[id] = c
	return c
}
func (s *Service) remember(h hello, host string) error {
	if len(h.ID) != 64 || h.ID == s.Identity.ID || h.Port < 1 || h.Port > 65535 || len(h.Name) > 128 {
		return errors.New("invalid device")
	}
	network := "lan"
	if ip, e := netip.ParseAddr(host); e == nil {
		if netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip) {
			network = "tailscale"
		}
	}
	p := Peer{OS: h.OS, Network: network, ID: h.ID, Name: h.Name, Address: "https://" + net.JoinHostPort(host, strconv.Itoa(h.Port)), Seen: time.Now()}
	s.mu.Lock()
	if _, exists := s.peers[p.ID]; !exists && len(s.peers) >= 256 {
		s.mu.Unlock()
		return errors.New("设备数量超过上限")
	}
	p.Verified = s.trusted[p.ID]
	s.peers[p.ID] = p
	s.devices[p.ID] = p
	s.mu.Unlock()
	s.notify()
	return nil
}
func (s *Service) Connect(ctx context.Context, address, pin string) error {
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("请输入主机 IP:端口")
	}
	if len(pin) != 64 {
		return errors.New("请输入完整的 64 位设备指纹")
	}
	b, _ := json.Marshal(hello{OS: runtime.GOOS, Protocol: "lantern/1", ID: s.Identity.ID, Name: s.Name, Port: s.port})
	req, err := http.NewRequestWithContext(ctx, "POST", u.Scheme+"://"+u.Host+"/v1/hello", strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	res, err := s.client(pin).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("连接失败：%s", res.Status)
	}
	var h hello
	if err = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&h); err != nil {
		return err
	}
	if h.ID != pin {
		return errors.New("设备身份不一致")
	}
	return s.remember(h, u.Hostname())
}
func (s *Service) handleHello(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	id := requestPeer(r)
	if id == "" {
		w.WriteHeader(401)
		return
	}
	var h hello
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&h); err != nil || h.ID != id {
		http.Error(w, "invalid identity", 400)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		w.WriteHeader(400)
		return
	}
	if err = s.remember(h, host); err != nil {
		w.WriteHeader(400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(hello{OS: runtime.GOOS, Protocol: "lantern/1", ID: s.Identity.ID, Name: s.Name, Port: s.port})
}
func requestPeer(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	return core.Fingerprint(r.TLS.PeerCertificates[0])
}
func (s *Service) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.Peer(requestPeer(r)); err != nil {
			http.Error(w, "send hello first", 403)
			return
		}
		s.touch(requestPeer(r))
		next.ServeHTTP(w, r)
		s.notify()
	})
}
func (s *Service) Send(ctx context.Context, peer, text string) error {
	p, err := s.Peer(peer)
	if err != nil {
		return err
	}
	id := core.ID()
	if _, err = s.Store.Add(core.Message{ID: id, Peer: peer, Text: text, State: core.Pending, Created: time.Now()}); err != nil {
		return err
	}
	s.notify()
	err = core.Send(ctx, s.client(peer), p.Address, id, text)
	state := core.Delivered
	if err == nil {
		s.touch(peer)
	}
	if err != nil {
		state = core.Failed
	}
	s.Store.Delivery(peer, id, state)
	s.notify()
	return err
}

// ReadVisible acknowledges only IDs explicitly reported visible by the UI.
func (s *Service) ReadVisible(ctx context.Context, peer string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	p, err := s.Peer(peer)
	if err != nil {
		return err
	}
	if err = core.SendRead(ctx, s.client(peer), p.Address, ids); err != nil {
		return err
	}
	s.Store.MarkReadIDs(peer, ids)
	s.notify()
	return nil
}

func (s *Service) Retry(ctx context.Context, peer, id string) error {
	p, err := s.Peer(peer)
	if err != nil {
		return err
	}
	for _, m := range s.Store.Messages(peer) {
		if m.ID == id && !m.Incoming && m.FileID == "" && m.State == core.Failed {
			s.Store.Delivery(peer, id, core.Pending)
			s.notify()
			err = core.Send(ctx, s.client(peer), p.Address, id, m.Text)
			state := core.Delivered
			if err != nil {
				state = core.Failed
			}
			s.Store.Delivery(peer, id, state)
			s.notify()
			return err
		}
	}
	return errors.New("消息不可重试")
}

func (s *Service) touch(id string) {
	s.mu.Lock()
	if p, ok := s.peers[id]; ok {
		p.Seen = time.Now()
		s.peers[id] = p
	}
	s.mu.Unlock()
}
