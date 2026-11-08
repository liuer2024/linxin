package lan

import (
	"context"
	"testing"
	"time"
)

func TestDiscoveryDoesNotCreateConversation(t *testing.T) {
	a := testService(t)
	b := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := hello{ID: b.Identity.ID, Name: b.Name, Port: b.Port(), Protocol: "lantern/1"}
	if err := a.probeDiscovered(ctx, h, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if len(a.Devices()) != 1 || len(a.Peers()) != 0 || len(b.Peers()) != 0 || len(a.Store.Conversations()) != 0 {
		t.Fatal("discovery created a conversation")
	}
	if err := a.ConnectDevice(ctx, b.Identity.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(ctx, b.Identity.ID, "发现后聊天"); err != nil {
		t.Fatal(err)
	}
	if len(b.Store.Conversations()) != 1 {
		t.Fatal("message did not create conversation")
	}
}
func TestMulticastDiscoveryTwoInstances(t *testing.T) {
	a := testService(t)
	b := testService(t)
	a.Discover()
	b.Discover()
	find := func(s *Service, id string) (Peer, bool) {
		for _, p := range s.Devices() {
			if p.ID == id {
				return p, true
			}
		}
		return Peer{}, false
	}
	deadline := time.Now().Add(28 * time.Second)
	first := time.Time{}
	for time.Now().Before(deadline) {
		pa, okA := find(a, b.Identity.ID)
		_, okB := find(b, a.Identity.ID)
		if okA && okB {
			if first.IsZero() {
				first = pa.Seen
			}
			// Check sustained refresh, not just one initial announcement or a third
			// application's presence in the same test machine's discovery list.
			if pa.Seen.Sub(first) >= 8*time.Second {
				return
			}
		}
		if time.Until(deadline) < 26*time.Second && a.DiscoveryStatus().Interfaces == 0 {
			t.Skip("no multicast interface: " + a.DiscoveryStatus().Error)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("mutual discovery / refresh timed out: A=%+v B=%+v", a.DiscoveryStatus(), b.DiscoveryStatus())
}
func TestAnnouncementValidation(t *testing.T) {
	valid := hello{ID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Port: 47832, Protocol: "lantern/1"}
	if !validAnnouncement(valid) {
		t.Fatal("valid announcement rejected")
	}
	valid.ID = "../../not-an-identity"
	if validAnnouncement(valid) {
		t.Fatal("invalid identity accepted")
	}
}
