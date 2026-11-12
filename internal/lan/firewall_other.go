//go:build !windows

package lan

// Only Windows ships a host firewall that blocks inbound LAN traffic by default.
func firewallHint(udpPort, tcpPort int) string { return "" }
