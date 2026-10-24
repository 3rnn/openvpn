package main

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var connectionIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,127}$`)

// Only data-plane client options are accepted. In particular, script/plugin,
// include/config and filesystem directives must never reach OpenVPN.
func validateClientDirectives(config string) error {
	if len(config) > 65536 {
		return fmt.Errorf("client configuration too large")
	}
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.ContainsAny(line, "\r\x00\\") {
			return fmt.Errorf("invalid client configuration")
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "push":
			rest := strings.TrimSpace(strings.TrimPrefix(line, "push"))
			if len(rest) < 2 || rest[0] != '"' || rest[len(rest)-1] != '"' || strings.Contains(rest[1:len(rest)-1], "\"") {
				return fmt.Errorf("push must contain one quoted data option")
			}
			option := strings.Fields(rest[1 : len(rest)-1])
			if len(option) == 0 {
				return fmt.Errorf("empty push")
			}
			switch option[0] {
			case "route", "route-ipv6", "dhcp-option", "redirect-gateway", "redirect-private", "block-outside-dns", "topology", "ping", "ping-restart", "inactive", "tun-mtu", "mssfix":
			default:
				return fmt.Errorf("unsupported pushed option")
			}
		case "route", "route-ipv6", "iroute", "iroute-ipv6", "ifconfig-push", "ifconfig-ipv6-push", "push-reset", "push-remove", "disable":
		default:
			return fmt.Errorf("unsupported client directive: %s", fields[0])
		}
	}
	return nil
}

// Atomic policy snapshot keyed by authenticated identity AND certificate name.
// OpenVPN's client-connect writes each snapshot to its own per-connection target.
func writeClientConnectionConfig(username, commonName, ipaddr, config string) error {
	if !connectionIdentity.MatchString(username) || !connectionIdentity.MatchString(commonName) {
		return fmt.Errorf("invalid connection identity")
	}
	if err := validateClientDirectives(config); err != nil {
		return err
	}
	var prefix string
	if ipaddr != "" {
		ip, err := netip.ParseAddr(ipaddr)
		if err != nil || !ip.Is4() {
			return fmt.Errorf("invalid static IPv4 address")
		}
		prefix = "ifconfig-push " + ip.String() + " ${ifconfig_netmask}\n"
	}
	dir := filepath.Join(ovData, "connections")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".policy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(prefix + config + "\n"); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	key := sha256.Sum256([]byte(username + "\x00" + commonName))
	return os.Rename(f.Name(), filepath.Join(dir, fmt.Sprintf("%x.conf", key)))
}
