package lan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mac2win/internal/core"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Transfer struct {
	ID       string    `json:"id"`
	Peer     string    `json:"-"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	SHA256   string    `json:"sha256"`
	Incoming bool      `json:"-"`
	State    string    `json:"-"`
	Done     int64     `json:"-"`
	Error    string    `json:"-"`
	Path     string    `json:"-"`
	Created  time.Time `json:"-"`
}

func (s *Service) Transfers(peer string) []Transfer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Transfer{}
	for _, f := range s.files {
		if f.Peer == peer {
			out = append(out, *f)
		}
	}
	return out
}
func fileKey(peer, id string) string { return peer + "/" + id }
func (s *Service) updateFile(peer, id, state string, done int64, err error) {
	s.mu.Lock()
	if f := s.files[fileKey(peer, id)]; f != nil {
		if f.State == "已完成" && (state == "已发送" || state == "传输中") {
			s.mu.Unlock()
			return
		}
		f.State = state
		f.Done = done
		if err != nil {
			f.Error = err.Error()
		}
	}
	s.mu.Unlock()
	s.notify()
}
func (s *Service) Offer(ctx context.Context, peer, path string) error {
	p, err := s.Peer(peer)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() {
		return errors.New("目前仅支持普通文件")
	}
	h := sha256.New()
	if _, err = io.Copy(h, &contextReader{ctx: ctx, r: f}); err != nil {
		return err
	}
	offer := Transfer{ID: core.ID(), Peer: peer, Name: stat.Name(), Size: stat.Size(), SHA256: hex.EncodeToString(h.Sum(nil)), Path: path, State: "等待接收", Created: time.Now()}
	s.mu.Lock()
	s.files[fileKey(peer, offer.ID)] = &offer
	s.mu.Unlock()
	_, _ = s.Store.Add(core.Message{ID: offer.ID, Peer: peer, Text: offer.Name, FileID: offer.ID, State: core.Pending, Created: time.Now()})
	s.notify()
	b, _ := json.Marshal(offer)
	req, err := http.NewRequestWithContext(ctx, "POST", p.Address+"/v1/files/offer", strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	res, err := s.client(peer).Do(req)
	if err == nil {
		defer res.Body.Close()
		if res.StatusCode != 204 {
			err = fmt.Errorf("发送邀请失败：%s", res.Status)
		}
	}
	if err != nil {
		s.updateFile(peer, offer.ID, "失败", 0, err)
		s.Store.Delivery(peer, offer.ID, core.Failed)
	} else {
		s.Store.Delivery(peer, offer.ID, core.Delivered)
	}
	s.notify()
	return err
}
func (s *Service) handleOffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	peer := requestPeer(r)
	if _, err := s.Peer(peer); err != nil {
		w.WriteHeader(403)
		return
	}
	var f Transfer
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&f); err != nil {
		w.WriteHeader(400)
		return
	}
	if len(f.ID) != 32 || len(f.SHA256) != 64 || f.Size < 0 || len(f.Name) > 255 || f.Name == "" || strings.ContainsAny(f.Name, "/\\\x00") || f.Name == "." || f.Name == ".." {
		http.Error(w, "invalid file metadata", 400)
		return
	}
	f.Peer = peer
	f.Incoming = true
	f.State = "等待接收"
	f.Created = time.Now()
	s.mu.Lock()
	if len(s.files) >= 1000 {
		s.mu.Unlock()
		http.Error(w, "too many offers", 429)
		return
	}
	key := fileKey(peer, f.ID)
	if s.files[key] == nil {
		s.files[key] = &f
	}
	s.mu.Unlock()
	_, _ = s.Store.Add(core.Message{ID: f.ID, Peer: peer, Text: f.Name, FileID: f.ID, Incoming: true, State: core.Delivered, Created: f.Created})
	s.notify()
	w.WriteHeader(204)
}
func (s *Service) handleContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	peer := requestPeer(r)
	id := strings.TrimPrefix(r.URL.Path, "/v1/files/content/")
	s.mu.RLock()
	ptr := s.files[fileKey(peer, id)]
	var f Transfer
	if ptr != nil {
		f = *ptr
	}
	s.mu.RUnlock()
	if ptr == nil || f.Incoming {
		w.WriteHeader(404)
		return
	}
	file, err := os.Open(f.Path)
	if err != nil {
		http.Error(w, "file unavailable", 410)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != f.Size {
		http.Error(w, "file changed", 409)
		return
	}
	s.touch(peer)
	s.updateFile(peer, id, "传输中", 0, nil)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprint(f.Size))
	n, err := io.Copy(w, &progressReader{r: &contextReader{ctx: r.Context(), r: file}, update: func(n int64) { s.updateFile(peer, id, "传输中", n, nil) }})
	if err != nil {
		s.updateFile(peer, id, "失败", n, err)
	} else {
		s.updateFile(peer, id, "已发送", n, nil)
	}
}

// Accept downloads into a private temporary file, verifies it, then publishes
// without overwriting an existing file. Destination is selected locally.
func (s *Service) Accept(ctx context.Context, peer, id, destination string) (err error) {
	p, err := s.Peer(peer)
	if err != nil {
		return err
	}
	s.mu.Lock()
	ptr := s.files[fileKey(peer, id)]
	if ptr == nil || !ptr.Incoming || ptr.State != "等待接收" {
		s.mu.Unlock()
		return errors.New("文件邀请不可接收")
	}
	f := *ptr
	ptr.State = "传输中"
	s.mu.Unlock()
	s.notify()
	var done int64
	defer func() {
		if err != nil {
			s.updateFile(peer, id, "失败", done, err)
		}
	}()
	client := *s.client(peer)
	client.Timeout = 0
	req, err := http.NewRequestWithContext(ctx, "GET", p.Address+"/v1/files/content/"+f.ID, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("接收失败：%s", res.Status)
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".lantern-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hash := sha256.New()
	reader := &progressReader{r: io.LimitReader(res.Body, f.Size+1), update: func(n int64) { done = n; s.updateFile(peer, id, "传输中", n, nil) }}
	done, err = io.Copy(io.MultiWriter(tmp, hash), reader)
	if err != nil {
		return err
	}
	if done != f.Size || hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
		return errors.New("文件校验失败，未保存")
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// Link is atomic and never overwrites existing destinations.
	if err = os.Link(tmp.Name(), destination); err != nil {
		return fmt.Errorf("保存失败（目标不可已存在，且磁盘需支持硬链接）：%w", err)
	}
	s.mu.Lock()
	s.files[fileKey(peer, id)].Path = destination
	s.mu.Unlock()
	s.updateFile(peer, id, "已完成", done, nil)
	_ = s.sendFileStatus(ctx, peer, id, "已完成")
	return nil
}
func (s *Service) Decline(peer, id string) {
	s.mu.Lock()
	if f := s.files[fileKey(peer, id)]; f != nil && f.Incoming && f.State == "等待接收" {
		f.State = "已拒绝"
	}
	s.mu.Unlock()
	s.notify()
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	_ = s.sendFileStatus(ctx, peer, id, "已拒绝")
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type progressReader struct {
	r      io.Reader
	n      int64
	last   time.Time
	update func(int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	if time.Since(r.last) > 150*time.Millisecond {
		r.last = time.Now()
		r.update(r.n)
	}
	return n, err
}

type fileStatus struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

func (s *Service) sendFileStatus(ctx context.Context, peer, id, state string) error {
	p, err := s.Peer(peer)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(fileStatus{ID: id, State: state})
	req, err := http.NewRequestWithContext(ctx, "POST", p.Address+"/v1/files/status", strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	res, err := s.client(peer).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 204 {
		return errors.New("file status rejected")
	}
	return nil
}
func (s *Service) handleFileStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	peer := requestPeer(r)
	var st fileStatus
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&st) != nil || (st.State != "已完成" && st.State != "已拒绝") {
		w.WriteHeader(400)
		return
	}
	s.mu.Lock()
	f := s.files[fileKey(peer, st.ID)]
	if f == nil || f.Incoming {
		s.mu.Unlock()
		w.WriteHeader(404)
		return
	}
	f.State = st.State
	s.mu.Unlock()
	s.notify()
	w.WriteHeader(204)
}

func (s *Service) AllTransfers() []Transfer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Transfer, 0, len(s.files))
	for _, f := range s.files {
		out = append(out, *f)
	}
	return out
}
