package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// Execute the test binary itself as a fake installed CLI. This exercises the
// real subprocess boundary on Windows/macOS without shell-dependent fixtures.
func TestMain(m *testing.M) {
	if mode := os.Getenv("LANTERN_TEST_TAILSCALE"); mode != "" {
		if len(os.Args) != 3 || os.Args[1] != "status" || os.Args[2] != "--json" {
			os.Exit(12)
		}
		switch mode {
		case "running":
			data, _ := os.ReadFile("testdata/status.json")
			os.Stdout.Write(data)
		case "login":
			fmt.Print(`{"BackendState":"NeedsLogin"}`)
			os.Exit(1)
		case "malformed":
			fmt.Print("not JSON")
		case "denied":
			fmt.Fprintln(os.Stderr, "permission denied: private diagnostics")
			os.Exit(1)
		case "slow":
			time.Sleep(10 * time.Second)
		case "large":
			fmt.Print(strings.Repeat("x", maxStatusBytes+1))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestParseAndAddress(t *testing.T) {
	data, err := os.ReadFile("testdata/status.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseStatus(data)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Ready() || len(s.Devices) != 3 {
		t.Fatalf("unexpected status: %+v", s)
	}
	if !s.Devices[0].Online || s.Devices[2].Online {
		t.Fatal("online peers not sorted first")
	}
	foundV6 := false
	for _, d := range s.Devices {
		if d.ID == "ipv6-node" {
			addr, err := d.Address(47832)
			if err != nil || addr != "[fd7a:115c:a1e0::4]:47832" {
				t.Fatal(addr, err)
			}
			foundV6 = true
		}
		if strings.HasSuffix(d.DNSName, ".") {
			t.Fatal("DNS trailing dot")
		}
	}
	if !foundV6 {
		t.Fatal("IPv6 peer lost")
	}
	for _, port := range []int{0, -1, 65536} {
		if _, err = s.Devices[0].Address(port); err == nil {
			t.Fatal("invalid port accepted")
		}
	}
	if _, err = (Device{IPs: []string{"127.0.0.1"}}).Address(47832); err == nil {
		t.Fatal("non-tailnet address accepted")
	}
}
func TestStatusStatesAndMalformed(t *testing.T) {
	for _, state := range []string{"Stopped", "NeedsLogin", "NeedsMachineAuth", "Starting", "FutureState"} {
		data, _ := json.Marshal(map[string]string{"BackendState": state})
		s, err := ParseStatus(data)
		if err != nil || s.Ready() || s.Summary() == "" {
			t.Fatal(state, s, err)
		}
	}
	for _, data := range []string{"{}", "null", "[1]", `{"BackendState":"Running"} {}`, "broken"} {
		if _, err := ParseStatus([]byte(data)); err == nil {
			t.Fatal("invalid status accepted", data)
		}
	}
}
func TestCLI(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"running", "login", "malformed", "denied", "large", "slow"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("LANTERN_TEST_TAILSCALE", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if mode == "slow" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 40*time.Millisecond)
			}
			defer cancel()
			s, err := (Client{Command: exe}).Status(ctx)
			switch mode {
			case "running":
				if err != nil || !s.Ready() {
					t.Fatal(s, err)
				}
			case "login":
				if err != nil || s.BackendState != "NeedsLogin" {
					t.Fatal(s, err)
				}
			default:
				if err == nil {
					t.Fatal("expected error")
				}
				if strings.Contains(err.Error(), "private diagnostics") {
					t.Fatal("raw CLI stderr leaked")
				}
			}
		})
	}
}

func TestOutputBoundCannotBypassWrite(t *testing.T) {
	b := &boundedBuffer{limit: 32}
	_, err := io.Copy(b, io.LimitReader(strings.NewReader(strings.Repeat("x", 128)), 128))
	if err == nil || b.Len() > 32 {
		t.Fatal("output cap bypassed", b.Len(), err)
	}
}
