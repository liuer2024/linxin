package lan

import (
	"context"
	"mac2win/internal/core"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTailscaleEndpointFlow(t *testing.T) {
	a := testService(t)
	b := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	address := b.listener.Addr().String()
	info, err := a.Probe(ctx, address, "")
	if err != nil || info.ID != b.Identity.ID {
		t.Fatal(info, err)
	}
	if len(b.Peers()) != 0 {
		t.Fatal("public metadata probe created a session")
	}
	peer, err := a.ConnectTailscale(ctx, address, "stable-tailscale-node")
	if err != nil {
		t.Fatal(err)
	}
	if peer.Verified {
		t.Fatal("first contact falsely marked verified")
	}
	if err = a.Send(ctx, peer.ID, "跨网络连接流程"); err != nil {
		t.Fatal(err)
	}
	incoming := b.Store.Messages(a.Identity.ID)
	if len(incoming) != 1 {
		t.Fatal("message lost")
	}
	if err = b.ReadVisible(ctx, a.Identity.ID, []string{incoming[0].ID}); err != nil {
		t.Fatal(err)
	}
	if a.Store.Messages(b.Identity.ID)[0].State != core.Read {
		t.Fatal("read receipt lost")
	}
	file := filepath.Join(t.TempDir(), "tailscale-test.txt")
	if err = os.WriteFile(file, []byte("Tailscale connection adapter"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = a.Offer(ctx, peer.ID, file); err != nil {
		t.Fatal(err)
	}
	offer := b.Transfers(a.Identity.ID)[0]
	dest := filepath.Join(t.TempDir(), "received.txt")
	if err = b.Accept(ctx, a.Identity.ID, offer.ID, dest); err != nil {
		t.Fatal(err)
	}
	pins, err := loadEndpointPins(a.dir)
	if err != nil || pins["stable-tailscale-node"] != b.Identity.ID {
		t.Fatal(pins, err)
	}
	// Same device at a changed address keeps the application identity binding;
	// a replacement application identity must fail before sending a hello.
	c := testService(t)
	if _, err = a.ConnectTailscale(ctx, c.listener.Addr().String(), "stable-tailscale-node"); err == nil {
		t.Fatal("changed application identity accepted")
	}
	if len(c.Peers()) != 0 {
		t.Fatal("identity mismatch sent a hello")
	}
	// Existing LAN and Tailscale routes refer to one conversation identity.
	if err = a.Connect(ctx, "https://"+address, b.Identity.ID); err != nil {
		t.Fatal(err)
	}
	if len(a.Peers()) != 1 {
		t.Fatal("duplicate LAN / Tailscale conversations")
	}
}
