//go:build windows

package lan

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// firewallHint checks whether Windows Defender Firewall is on without an
// inbound rule that admits the discovery and connection ports. It only reads
// the current configuration; adding rules needs an elevated prompt and is
// left to the user.
func firewallHint(udpPort, tcpPort int) string {
	profile, err := netsh("advfirewall", "show", "currentprofile")
	if err != nil {
		return ""
	}
	if !firewallEnabled(profile) {
		return ""
	}
	missing := []string{}
	if !firewallAllows("UDP", udpPort) {
		missing = append(missing, fmt.Sprintf("UDP %d（发现）", udpPort))
	}
	if !firewallAllows("TCP", tcpPort) {
		missing = append(missing, fmt.Sprintf("TCP %d（连接）", tcpPort))
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("Windows 防火墙可能拦截了 %s。首次运行请在防火墙提示中勾选「专用网络」，或以管理员身份执行：\n"+
		"netsh advfirewall firewall add rule name=\"邻信 发现\" dir=in action=allow protocol=UDP localport=%d\n"+
		"netsh advfirewall firewall add rule name=\"邻信 连接\" dir=in action=allow protocol=TCP localport=%d",
		strings.Join(missing, "、"), udpPort, tcpPort)
}

func firewallEnabled(profile string) bool {
	for _, line := range strings.Split(profile, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "State") {
			return strings.EqualFold(fields[len(fields)-1], "ON")
		}
	}
	return false
}

// firewallAllows reports whether any enabled inbound allow rule covers the
// port, matching by name ("邻信 ..."), by the application's own exe or by an
// explicit port entry.
func firewallAllows(protocol string, port int) bool {
	out, err := netsh("advfirewall", "firewall", "show", "rule", "name=all", "dir=in", "verbose")
	if err != nil {
		return true
	}
	for _, rule := range strings.Split(out, "\n\n") {
		if !strings.Contains(rule, "Enabled:") || !ruleField(rule, "Enabled:", "Yes") || !ruleField(rule, "Action:", "Allow") {
			continue
		}
		if !ruleField(rule, "Protocol:", protocol) && !ruleField(rule, "Protocol:", "Any") {
			continue
		}
		local := ruleValue(rule, "LocalPort:")
		if local == "Any" || portListed(local, port) {
			return true
		}
	}
	return false
}

func ruleField(rule, key, want string) bool { return strings.EqualFold(ruleValue(rule, key), want) }
func ruleValue(rule, key string) string {
	for _, line := range strings.Split(rule, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key) {
			return strings.TrimSpace(strings.TrimPrefix(line, key))
		}
	}
	return ""
}
func portListed(list string, port int) bool {
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == fmt.Sprint(port) {
			return true
		}
		if lo, hi, ok := strings.Cut(item, "-"); ok {
			var a, b int
			if _, e := fmt.Sscanf(lo+" "+hi, "%d %d", &a, &b); e == nil && a <= port && port <= b {
				return true
			}
		}
	}
	return false
}

func netsh(args ...string) (string, error) {
	cmd := exec.Command("netsh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	return string(out), err
}
