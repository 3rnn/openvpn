package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The web editor changes data-plane options, not the process's execution or
// filesystem authority. Sensitive installation-owned directives must remain
// byte-for-byte identical (including multiplicity); new/unknown options fail closed.
func validateVPNConfigUpdate(original, content string, client bool) error {
	old, err := protectedVPNLines(original, client)
	if err != nil {
		return fmt.Errorf("existing configuration requires offline review: %w", err)
	}
	next, err := protectedVPNLines(content, client)
	if err != nil {
		return err
	}
	if len(old) != len(next) {
		return fmt.Errorf("installation-owned directives cannot be changed")
	}
	for line, n := range old {
		if next[line] != n {
			return fmt.Errorf("installation-owned directives cannot be changed")
		}
	}
	return nil
}

func protectedVPNLines(content string, client bool) (map[string]int, error) {
	if len(content) > 1<<20 || strings.ContainsAny(content, "\r\x00\\") {
		return nil, fmt.Errorf("invalid configuration encoding or size")
	}
	protected := map[string]int{}
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		key := fields[0]
		if strings.HasPrefix(key, "<") {
			if !client || !(line == "<ca>" || line == "<cert>" || line == "<key>" || line == "<tls-crypt>" || line == "<tls-auth>") {
				return nil, fmt.Errorf("unsupported inline block")
			}
			end := "</" + line[1:]
			block := line
			closed := false
			for i++; i < len(lines); i++ {
				block += "\n" + lines[i]
				if strings.TrimSpace(lines[i]) == end {
					closed = true
					break
				}
			}
			if !closed {
				return nil, fmt.Errorf("unterminated inline block")
			}
			protected[block]++
			continue
		}
		switch key {
		case "push":
			if client {
				return nil, fmt.Errorf("server-only directive")
			}
			if err := validateClientDirectives(line); err != nil {
				return nil, err
			}
			continue
		case "client-connect", "client-disconnect", "learn-address":
			if client || line != key+" /usr/bin/docker-entrypoint.sh" {
				return nil, fmt.Errorf("unsupported runtime hook")
			}
		case "auth-user-pass-verify":
			if client || line != "auth-user-pass-verify /usr/lib/openvpn/plugins/openvpn-auth via-env" {
				return nil, fmt.Errorf("unsupported authentication hook")
			}
		case "script-security":
			if client || line != "script-security 3" {
				return nil, fmt.Errorf("script policy is installation-owned")
			}
		case "management":
			if client || len(fields) != 3 || fields[1] != "127.0.0.1" || !validVPNPort(fields[2]) {
				return nil, fmt.Errorf("management must be installation-owned loopback TCP")
			}
		case "ca", "cert", "key", "dh", "tls-auth", "tls-crypt", "tls-crypt-v2", "crl-verify", "status", "client-config-dir", "ifconfig-pool-persist", "log", "log-append", "writepid", "tmp-dir", "setenv":
			// Existing path/environment authority can be retained, never introduced,
			// redirected, duplicated or removed through this editor.
		case "auth-user-pass":
			if !client || len(fields) != 1 {
				return nil, fmt.Errorf("external credential files are unsupported")
			}
			continue
		case "dev":
			if len(fields) != 2 || (fields[1] != "tun" && fields[1] != "tap") {
				return nil, fmt.Errorf("unsupported device")
			}
			continue
		case "port", "proto", "local", "remote", "client", "tls-client", "tls-server", "server", "server-ipv6", "topology", "persist-key", "persist-tun", "keepalive", "duplicate-cn", "client-to-client", "max-clients", "verb", "mute", "status-version", "auth", "cipher", "data-ciphers", "data-ciphers-fallback", "tls-groups", "tls-version-min", "tls-version-max", "remote-cert-tls", "verify-x509-name", "resolv-retry", "nobind", "explicit-exit-notify", "route", "route-ipv6", "route-gateway", "redirect-gateway", "redirect-private", "dhcp-option", "block-outside-dns", "ping", "ping-restart", "ping-timer-rem", "inactive", "tun-mtu", "mssfix", "sndbuf", "rcvbuf", "fast-io", "float", "reneg-sec", "auth-nocache", "auth-retry", "auth-token", "key-direction", "connect-retry", "connect-timeout", "connect-retry-max", "remote-random", "remote-random-hostname", "pull", "pull-filter", "route-nopull", "route-delay":
			continue
		default:
			return nil, fmt.Errorf("unsupported directive: %s", key)
		}
		protected[line]++
	}
	return protected, nil
}

func validVPNPort(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n > 0 && n <= 65535 && strconv.Itoa(n) == value
}

// Validate before any settings side effect, not merely when auto-update happens
// to be enabled. Otherwise stored newline payloads become active on a later toggle.
func validateVPNSetting(key, value string) error {
	if !strings.HasPrefix(key, "openvpn.") {
		return nil
	}
	if strings.ContainsAny(value, "\r\n\x00\\\"'") {
		return fmt.Errorf("invalid OpenVPN setting")
	}
	switch key {
	case "openvpn.ovpn_proto":
		switch value {
		case "udp", "udp6", "tcp-server", "tcp6-server":
		default:
			return fmt.Errorf("invalid OpenVPN protocol")
		}
	case "openvpn.ovpn_port":
		if !validVPNPort(value) {
			return fmt.Errorf("invalid port")
		}
	case "openvpn.ovpn_max_clients":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("invalid client limit")
		}
	case "openvpn.ovpn_management":
		host, port, err := net.SplitHostPort(value)
		if err != nil || host != "127.0.0.1" || !validVPNPort(port) {
			return fmt.Errorf("management requires loopback TCP")
		}
	}
	return nil
}

// Backup and replace only after validation, using private same-directory files.
func updateServerConfig(content string) error {
	target := filepath.Join(ovData, "server.conf")
	original, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if err = validateVPNConfigUpdate(string(original), content, false); err != nil {
		return err
	}
	if err = atomicPrivateConfig(target+".bak", original); err != nil {
		return err
	}
	return atomicPrivateConfig(target, []byte(content))
}
func writeClientCCD(name, content string) error {
	if !connectionIdentity.MatchString(name) {
		return fmt.Errorf("invalid client name")
	}
	if err := validateClientDirectives(content); err != nil {
		return err
	}
	dir := filepath.Join(ovData, "ccd")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return atomicRootConfig(root, name, []byte(content))
}

func atomicRootConfig(root *os.Root, target string, content []byte) error {
	name := ".config-" + secureToken()
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	if _, err = f.Write(content); err != nil {
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
	return root.Rename(name, target)
}

func atomicPrivateConfig(target string, content []byte) error {
	f, err := os.CreateTemp(filepath.Dir(target), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(content); err != nil {
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
	return os.Rename(f.Name(), target)
}
