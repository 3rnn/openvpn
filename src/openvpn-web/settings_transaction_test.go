package main

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestSettingsMissingServerDoesNotPublishCredentials(t *testing.T) {
	b := runtimeBrowser(t, ovpn{})
	target := viper.ConfigFileUsed()
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	password, version, live := viper.GetString("system.base.admin_password"), viper.GetString("system.base.admin_session_version"), adminPassword
	w := b.request("POST", "/settings", url.Values{"currentPass": {"LongPassword!123"}, "system.base.admin_password": {"ChangedPassword!123"}, "system.base.auto_update_ovpn_config": {"true"}})
	if w.Code != 500 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	after, _ := os.ReadFile(target)
	if string(after) != string(before) || password != viper.GetString("system.base.admin_password") || version != viper.GetString("system.base.admin_session_version") || live != adminPassword {
		t.Fatal("failed server preparation published JSON/live credentials")
	}
}

func TestSettingsInvalidDocumentDoesNotReloadOrRewriteServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan string, 12)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.SetDeadline(time.Now().Add(time.Second))
			line, _ := bufio.NewReader(conn).ReadString('\n')
			commands <- line
			conn.Write([]byte("SUCCESS: reloaded\r\n"))
			conn.Close()
		}
	}()
	b := runtimeBrowser(t, ovpn{address: listener.Addr().String()})
	t.Cleanup(func() {
		select {
		case command := <-commands:
			t.Errorf("reload before commit: %q", command)
		default:
		}
	})
	target := filepath.Join(ovData, "server.conf")
	original := "port 1194\nstatus-version 2\n"
	if err := os.WriteFile(target, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	// Repeat to exercise different randomized form-map orders in the old implementation.
	for i := 0; i < 12; i++ {
		w := b.request("POST", "/settings", url.Values{"currentPass": {"LongPassword!123"}, "system.base.max_duplicate_login": {"2"}, "openvpn.ovpn_subnet": {"invalid"}})
		if w.Code < 400 {
			t.Fatalf("invalid settings accepted: %d", w.Code)
		}
		got, _ := os.ReadFile(target)
		if string(got) != original {
			t.Fatal("validation failure rewrote server before commit/reload")
		}
	}
}

func TestSettingsPublicationFailureRollsBackFilesAndCredentials(t *testing.T) {
	for _, mode := range []string{"before-server", "after-server", "rollback-failure"} {
		t.Run(mode, func(t *testing.T) {
			runtimeBrowser(t, ovpn{})
			server := filepath.Join(ovData, "server.conf")
			originalServer := []byte("port 1194\n")
			if err := os.WriteFile(server, originalServer, 0600); err != nil {
				t.Fatal(err)
			}
			target := viper.ConfigFileUsed()
			originalJSON, _ := os.ReadFile(target)
			password, live := viper.GetString("system.base.admin_password"), adminPassword
			staged := viper.New()
			staged.SetConfigFile(target)
			staged.MergeConfigMap(viper.AllSettings())
			staged.Set("system.base.admin_password", "different-hash")
			staged.Set("system.base.auto_update_ovpn_config", false)
			staged.Set("system.base.max_duplicate_login", 2)
			failed := false
			writer := func(p string, data []byte) error {
				if p == server && !failed {
					failed = true
					if mode != "before-server" {
						if err := atomicPrivateConfig(p, data); err != nil {
							return err
						}
					}
					return fmt.Errorf("injected server publication failure")
				}
				if failed && mode == "rollback-failure" {
					return fmt.Errorf("injected rollback failure")
				}
				return atomicPrivateConfig(p, data)
			}
			reload, err := publishSettingsWithWriter(staged, writer)
			if err == nil || reload {
				t.Fatal("failed publication succeeded/reloaded")
			}
			if viper.GetString("system.base.admin_password") != password || adminPassword != live {
				t.Fatal("failed publication changed live credential")
			}
			if mode == "rollback-failure" {
				if !strings.Contains(err.Error(), "recovery required") {
					t.Fatalf("no explicit recovery: %v", err)
				}
				backups, _ := filepath.Glob(filepath.Join(ovData, ".settings-recovery-*"))
				if len(backups) == 0 {
					t.Fatal("missing recovery snapshot")
				}
			} else {
				got, _ := os.ReadFile(target)
				serverGot, _ := os.ReadFile(server)
				if string(got) != string(originalJSON) || string(serverGot) != string(originalServer) {
					t.Fatal("rollback did not restore originals")
				}
			}
		})
	}
}

func TestPrepareSettingsVPNUsesStagedValuesWithoutEffects(t *testing.T) {
	runtimeBrowser(t, ovpn{})
	server := filepath.Join(ovData, "server.conf")
	original := []byte("port 1194\nserver 10.8.0.0 255.255.255.0\n")
	if err := os.WriteFile(server, original, 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	// Any accidental firewall execution is inert and recorded, never host-active.
	for _, name := range []string{"iptables-legacy", "iptables-nft", "ip6tables-legacy", "ip6tables-nft"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf called >> \"$CALLS\"\nexit 1\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("CALLS", calls)
	v := viper.New()
	for k, value := range map[string]any{
		"system.base.auto_update_ovpn_config": true, "system.base.web_port": "9443",
		"openvpn.ovpn_port": "1195", "openvpn.ovpn_proto": "udp", "openvpn.ovpn_subnet": "10.9.0.0/24",
		"openvpn.ovpn_ipv6": true, "openvpn.ovpn_subnet6": "fd00::/64", "openvpn.ovpn_gateway": true,
		"openvpn.ovpn_push_dns1": "1.1.1.1", "openvpn.ovpn_push_dns2": "8.8.8.8",
	} {
		v.Set(k, value)
	}
	cfg, _, err := prepareSettingsVPN(v)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Join(cfg.Lines, "\n")
	for _, want := range []string{"port 1195", "proto udp6", "server 10.9.0.0 255.255.255.0", "server-ipv6 fd00::/64", `push "dhcp-option DNS 1.1.1.1"`, `push "dhcp-option DNS 8.8.8.8"`, "http://127.0.0.1:9443/login"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing staged value %s", want)
		}
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatal("preparation executed firewall commands")
	}
	got, _ := os.ReadFile(server)
	if string(got) != string(original) {
		t.Fatal("preparation changed server")
	}
}
