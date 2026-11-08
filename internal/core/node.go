package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Node exposes the conversation protocol over an authenticated TLS connection.
// The desktop discovery/pairing layer supplies the pinned peer fingerprint.
type Node struct{ Store *Store }
type envelope struct {
	ID   string   `json:"id"`
	Text string   `json:"text"`
	IDs  []string `json:"ids"`
}

func Fingerprint(cert *x509.Certificate) string {
	h := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(h[:])
}
func PinnedClient(pin string) *http.Client {
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second, TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		// Verification is explicitly performed by the certificate pin below.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 || pin == "" || Fingerprint(cs.PeerCertificates[0]) != pin {
				return errors.New("peer certificate does not match pairing")
			}
			return nil
		},
	}}}
}
func (n *Node) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) { n.handle(w, r, false) })
	mux.HandleFunc("/v1/read", func(w http.ResponseWriter, r *http.Request) { n.handle(w, r, true) })
	return mux
}
func (n *Node) handle(w http.ResponseWriter, r *http.Request, receipt bool) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "client certificate required", 401)
		return
	}
	peer := Fingerprint(r.TLS.PeerCertificates[0])
	var e envelope
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "trailing data", 400)
		return
	}
	if receipt {
		if len(e.IDs) > 1000 {
			http.Error(w, "too many receipts", 400)
			return
		}
		n.Store.Receipt(peer, e.IDs)
	} else {
		if _, err := n.Store.Add(Message{ID: e.ID, Peer: peer, Text: e.Text, Incoming: true, State: Delivered, Created: time.Now()}); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
func Send(ctx context.Context, c *http.Client, address, id, text string) error {
	return post(ctx, c, address, "/v1/messages", envelope{ID: id, Text: text})
}
func SendRead(ctx context.Context, c *http.Client, address string, ids []string) error {
	return post(ctx, c, address, "/v1/read", envelope{IDs: ids})
}
func post(ctx context.Context, c *http.Client, address, path string, e envelope) error {
	if !strings.HasPrefix(address, "https://") {
		return errors.New("HTTPS required")
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(address, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 204 {
		return errors.New("peer rejected request: " + res.Status)
	}
	return nil
}
