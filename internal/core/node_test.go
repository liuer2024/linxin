package core

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestTLSMessageAndReceipt(t *testing.T) {
	store := NewStore()
	node := &Node{Store: store}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if errors.Is(err, os.ErrPermission) {
		t.Skip("sandbox disallows local listeners")
	}
	if err != nil {
		t.Fatal(err)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: node.Handler()}}
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()
	client := PinnedClient(Fingerprint(server.Certificate()))
	// Reuse the test certificate as a separate client's identity for this test.
	client.Transport.(*http.Transport).TLSClientConfig.Certificates = server.TLS.Certificates
	peer := Fingerprint(server.Certificate())
	for i := 0; i < 2; i++ {
		if err := Send(context.Background(), client, server.URL, "m1", "你好"); err != nil {
			t.Fatal(err)
		}
	}
	if store.Unread(peer) != 1 {
		t.Fatal("delivery or deduplication failed")
	}
	ids := store.MarkRead(peer)
	if len(ids) != 1 {
		t.Fatal("read failed")
	}
	store.Add(Message{ID: "out", Peer: peer, Text: "reply", State: Delivered})
	if err := SendRead(context.Background(), client, server.URL, []string{"out"}); err != nil {
		t.Fatal(err)
	}
	if store.Messages(peer)[1].State != Read {
		t.Fatal("receipt failed")
	}
	bad := PinnedClient("wrong")
	if err := Send(context.Background(), bad, server.URL, "x", "bad"); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}
