package lan

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mac2win/internal/core"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type deviceInfo struct {
	OS       string `json:"os"`
	Protocol string `json:"protocol"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Port     int    `json:"port"`
}

func (s *Service) handleIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	if requestPeer(r) == "" {
		w.WriteHeader(401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(deviceInfo{OS: runtime.GOOS, Protocol: "lantern/1", ID: s.Identity.ID, Name: s.Name, Port: s.port})
}

// Probe reads public device metadata only. First contact observes the TLS
// certificate, but does NOT mark it trusted. Subsequent probes pin the recorded
// application identity for the stable Tailscale device ID.
func (s *Service) Probe(ctx context.Context, address, expected string) (deviceInfo, error) {
	u, err := url.Parse("https://" + address)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return deviceInfo{}, errors.New("invalid peer address")
	}
	observed := ""
	transport := &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{s.Identity.Certificate},
		// This public-metadata probe captures the peer certificate. The actual hello,
		// messages and files always use the resulting strict certificate pin.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("missing device certificate")
			}
			observed = core.Fingerprint(cs.PeerCertificates[0])
			if expected != "" && observed != expected {
				return errors.New("对方邻信身份已变化，请通过可信渠道重新核对指纹")
			}
			return nil
		},
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String()+"/v1/identity", nil)
	if err != nil {
		return deviceInfo{}, err
	}
	res, err := client.Do(req)
	if err != nil {
		return deviceInfo{}, fmt.Errorf("无法连接对方邻信：%w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return deviceInfo{}, errors.New("对方未运行兼容的邻信版本，请确认双方已更新")
	}
	var info deviceInfo
	if err = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&info); err != nil {
		return info, errors.New("对方不是有效的邻信服务")
	}
	if info.Protocol != "lantern/1" || info.ID != observed || len(info.Name) > 128 || info.Port < 1 || info.Port > 65535 {
		return info, errors.New("邻信身份校验失败")
	}
	return info, nil
}

// ConnectTailscale is called only for a user-selected device returned by the
// local Tailscale CLI. The stable device ID survives Tailscale address changes.
func (s *Service) ConnectTailscale(ctx context.Context, address, nodeID string) (Peer, error) {
	if nodeID == "" || len(nodeID) > 512 {
		return Peer{}, errors.New("invalid Tailscale device ID")
	}
	s.mu.RLock()
	expected := s.endpointPins[nodeID]
	s.mu.RUnlock()
	info, err := s.Probe(ctx, address, expected)
	if err != nil {
		return Peer{}, err
	}
	if err = s.Connect(ctx, address, info.ID); err != nil {
		return Peer{}, err
	}
	s.mu.Lock()
	p := s.peers[info.ID]
	p.Network = "tailscale"
	s.peers[info.ID] = p
	err = s.saveEndpointPin(nodeID, info.ID)
	s.mu.Unlock()
	if err != nil {
		return Peer{}, err
	}
	return s.Peer(info.ID)
}

// Caller holds mu. Only public fingerprint bindings are persisted.
func (s *Service) saveEndpointPin(nodeID, pin string) error {
	saved := make(map[string]string, len(s.endpointPins)+1)
	for k, v := range s.endpointPins {
		saved[k] = v
	}
	saved[nodeID] = pin
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".tailscale-pins-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), filepath.Join(s.dir, "tailscale-peers.json")); err != nil {
		return err
	}
	s.endpointPins = saved
	return nil
}
func loadEndpointPins(dir string) (map[string]string, error) {
	pins := map[string]string{}
	data, err := os.ReadFile(filepath.Join(dir, "tailscale-peers.json"))
	if errors.Is(err, os.ErrNotExist) {
		return pins, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &pins); err != nil {
		return nil, err
	}
	if pins == nil {
		pins = map[string]string{}
	}
	return pins, nil
}
