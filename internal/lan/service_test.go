package lan

import (
	"context"
	"errors"
	"fmt"
	"mac2win/internal/core"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testService(t *testing.T) *Service {
	t.Helper()
	s, err := Start(t.TempDir(), "test", "127.0.0.1:0", nil)
	if errors.Is(err, os.ErrPermission) {
		t.Skip("sandbox disallows TCP listener")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func TestIdentitySurvivesRestart(t *testing.T) {
	d := t.TempDir()
	a, e := LoadIdentity(d)
	if e != nil {
		t.Fatal(e)
	}
	b, e := LoadIdentity(d)
	if e != nil {
		t.Fatal(e)
	}
	if a.ID != b.ID {
		t.Fatal("identity changed")
	}
}
func TestTwoDevices(t *testing.T) {
	a := testService(t)
	b := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	address := "https://" + b.listener.Addr().String()
	if err := a.Connect(ctx, address, b.Identity.ID); err != nil {
		t.Fatal(err)
	}
	if len(b.Peers()) != 1 {
		t.Fatal("reciprocal hello missing")
	}
	if err := a.Send(ctx, b.Identity.ID, "你好，Windows"); err != nil {
		t.Fatal(err)
	}
	if b.Store.Unread(a.Identity.ID) != 1 {
		t.Fatal("unread missing")
	}
	messages := b.Store.Messages(a.Identity.ID)
	if err := b.ReadVisible(ctx, a.Identity.ID, []string{messages[0].ID}); err != nil {
		t.Fatal(err)
	}
	if a.Store.Messages(b.Identity.ID)[0].State != core.Read {
		t.Fatal("read receipt missing")
	}
	source := filepath.Join(t.TempDir(), "test.txt")
	data := []byte("局域网传输\n")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Offer(ctx, b.Identity.ID, source); err != nil {
		t.Fatal(err)
	}
	incoming := b.Transfers(a.Identity.ID)
	if len(incoming) != 1 {
		t.Fatal("offer missing")
	}
	dest := filepath.Join(t.TempDir(), "received.txt")
	if err := b.Accept(ctx, a.Identity.ID, incoming[0].ID, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(data) {
		t.Fatal("file differs", err)
	}
	if a.Transfers(b.Identity.ID)[0].State != "已完成" {
		t.Fatal("completion receipt missing")
	}
	// Existing destination must survive a second incoming transfer.
	if err := a.Offer(ctx, b.Identity.ID, source); err != nil {
		t.Fatal(err)
	}
	for _, f := range b.Transfers(a.Identity.ID) {
		if f.State == "等待接收" {
			if err := b.Accept(ctx, a.Identity.ID, f.ID, dest); err == nil {
				t.Fatal("overwrote existing destination")
			}
		}
	}
	got, _ = os.ReadFile(dest)
	if string(got) != string(data) {
		t.Fatal("existing file changed")
	}
	if err := a.Connect(ctx, address, a.Identity.ID); err == nil {
		t.Fatal("wrong certificate pin accepted")
	}
}

func TestFileMutationAndDecline(t *testing.T) {
	a := testService(t)
	b := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Connect(ctx, "https://"+b.listener.Addr().String(), b.Identity.ID); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sample.bin")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Offer(ctx, b.Identity.ID, path); err != nil {
		t.Fatal(err)
	}
	f := b.Transfers(a.Identity.ID)[0]
	if err := os.WriteFile(path, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "output.bin")
	if err := b.Accept(ctx, a.Identity.ID, f.ID, dest); err == nil {
		t.Fatal("modified file accepted")
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid file published")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary file leaked", err)
	}
	if err := a.Offer(ctx, b.Identity.ID, path); err != nil {
		t.Fatal(err)
	}
	for _, f := range b.Transfers(a.Identity.ID) {
		if f.State == "等待接收" {
			b.Decline(a.Identity.ID, f.ID)
		}
	}
	found := false
	for _, f := range a.Transfers(b.Identity.ID) {
		if f.State == "已拒绝" {
			found = true
		}
	}
	if !found {
		t.Fatal("decline not acknowledged")
	}
	if err := a.VerifyPeer(b.Identity.ID); err != nil {
		t.Fatal(err)
	}
	trust, err := loadTrust(a.dir)
	if err != nil || !trust[b.Identity.ID] {
		t.Fatal("trust not persisted", err)
	}
}

func BenchmarkTransfer32MiB(b *testing.B) {
	a, err := Start(b.TempDir(), "sender", "127.0.0.1:0", nil)
	if errors.Is(err, os.ErrPermission) {
		b.Skip("listener unavailable")
	}
	if err != nil {
		b.Fatal(err)
	}
	defer a.Close()
	receiver, err := Start(b.TempDir(), "receiver", "127.0.0.1:0", nil)
	if err != nil {
		b.Fatal(err)
	}
	defer receiver.Close()
	ctx := context.Background()
	if err = a.Connect(ctx, "https://"+receiver.listener.Addr().String(), receiver.Identity.ID); err != nil {
		b.Fatal(err)
	}
	const length = 32 << 20
	source := filepath.Join(b.TempDir(), "source.bin")
	file, err := os.Create(source)
	if err != nil {
		b.Fatal(err)
	}
	if err = file.Truncate(length); err != nil {
		b.Fatal(err)
	}
	file.Close()
	destDir := b.TempDir()
	b.SetBytes(length)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = a.Offer(ctx, receiver.Identity.ID, source); err != nil {
			b.Fatal(err)
		}
		for _, f := range receiver.Transfers(a.Identity.ID) {
			if f.State == "等待接收" {
				dest := filepath.Join(destDir, fmt.Sprint(i))
				if err = receiver.Accept(ctx, a.Identity.ID, f.ID, dest); err != nil {
					b.Fatal(err)
				}
				os.Remove(dest)
			}
		}
	}
}
