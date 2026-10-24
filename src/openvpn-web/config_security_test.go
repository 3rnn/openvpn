package main

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestConfigEditingRejectsExecutableDirectives(t *testing.T) {
	authTestDB(t)
	configureTestAdmin(t)
	if err := db.AutoMigrate(&Group{}); err != nil {
		t.Fatal(err)
	}
	old := ovData
	ovData = t.TempDir()
	t.Cleanup(func() { ovData = old })
	os.MkdirAll(filepath.Join(ovData, "clients"), 0700)
	os.MkdirAll(filepath.Join(ovData, "ccd"), 0700)
	server := "port 1194\nclient-connect /usr/bin/docker-entrypoint.sh\nscript-security 3\nmanagement 127.0.0.1 7505\n"
	client := "client\ndev tun\nremote vpn.example 1194\n<key>\nfixture-private-material\n</key>\n"
	os.WriteFile(filepath.Join(ovData, "server.conf"), []byte(server), 0600)
	os.WriteFile(filepath.Join(ovData, "clients", "cert.ovpn"), []byte(client), 0600)
	b := newBrowser(setupRouter(ovpn{}))
	loginBrowser(t, b, "admin")
	b.csrf(t)
	cases := []struct {
		method, path   string
		values         url.Values
		target, before string
	}{
		{"POST", "/ovpn/server", url.Values{"action": {"updateConfig"}, "content": {server + "up /bin/evil\n"}}, "server.conf", server},
		{"PUT", "/ovpn/client/cert/config", url.Values{"content": {client + "plugin /evil.so\n"}}, "clients/cert.ovpn", client},
		{"PUT", "/ovpn/client/cert/ccd", url.Values{"content": {"config /evil\n"}}, "ccd/cert", ""},
		{"POST", "/ovpn/group", url.Values{"name": {"Default"}, "config": {"up /bin/evil"}}, "", ""},
		{"POST", "/settings", url.Values{"currentPass": {"LongPassword!123"}, "openvpn.ovpn_proto": {"udp\nup /bin/evil"}}, "server.conf", server},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			w := b.request(tc.method, tc.path, tc.values)
			if w.Code != 400 {
				t.Errorf("unsafe update returned %d: %s", w.Code, w.Body)
			}
			if tc.target != "" {
				got, err := os.ReadFile(filepath.Join(ovData, tc.target))
				if os.IsNotExist(err) && tc.before == "" {
					return
				}
				if err != nil || string(got) != tc.before {
					t.Errorf("rejected update mutated file: %q %v", got, err)
				}
			}
		})
	}
}

func TestConfigEditorsPreserveHooksAndPrivateMaterial(t *testing.T) {
	authTestDB(t)
	configureTestAdmin(t)
	old := ovData
	ovData = t.TempDir()
	t.Cleanup(func() { ovData = old })
	if err := os.MkdirAll(filepath.Join(ovData, "clients"), 0700); err != nil {
		t.Fatal(err)
	}
	server := "port 1194\nclient-connect /usr/bin/docker-entrypoint.sh\nscript-security 3\nmanagement 127.0.0.1 7505\n"
	client := "client\ndev tun\n<key>\nfixture-private-material\n</key>\nverb 2\n"
	if err := os.WriteFile(filepath.Join(ovData, "server.conf"), []byte(server), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ovData, "clients", "cert.ovpn"), []byte(client), 0644); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(setupRouter(ovpn{}))
	loginBrowser(t, b, "admin")
	b.csrf(t)
	for _, tc := range []struct {
		method, path, target, content string
		values                        url.Values
	}{
		{"POST", "/ovpn/server", "server.conf", strings.Replace(server, "1194", "1195", 1), url.Values{"action": {"updateConfig"}}},
		{"PUT", "/ovpn/client/cert/config", "clients/cert.ovpn", strings.Replace(client, "verb 2", "verb 3", 1), url.Values{}},
	} {
		tc.values.Set("content", tc.content)
		w := b.request(tc.method, tc.path, tc.values)
		if w.Code != 200 {
			t.Fatalf("safe update %d %s", w.Code, w.Body)
		}
		data, err := os.ReadFile(filepath.Join(ovData, tc.target))
		if err != nil || string(data) != tc.content {
			t.Fatalf("save: %q %v", data, err)
		}
		info, err := os.Stat(filepath.Join(ovData, tc.target))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Errorf("private config mode: %v %v", info, err)
		}
	}
	backup, err := os.ReadFile(filepath.Join(ovData, "server.conf.bak"))
	if err != nil || string(backup) != server {
		t.Fatalf("backup: %q %v", backup, err)
	}
}

func TestVPNConfigPolicyRejectsAuthorityChanges(t *testing.T) {
	server := "port 1194\nclient-connect /usr/bin/docker-entrypoint.sh\nscript-security 3\nmanagement 127.0.0.1 7505\nca /data/pki/ca.crt\n"
	for _, payload := range []string{"up /bin/evil", "down /bin/evil", "plugin /evil.so", "config /evil.conf", "--config /evil.conf", "management 0.0.0.0 7505", "log /etc/cron.d/evil", "tls-export-cert /etc", "cd /evil", "chroot /evil", "dev /etc/evil", "setenv PATH /evil", "push \"up /bin/evil\"", "<connection>\nconfig /evil\n</connection>", "client-connect /usr/bin/docker-entrypoint.sh extra"} {
		t.Run(payload, func(t *testing.T) {
			if err := validateVPNConfigUpdate(server, server+payload+"\n", false); err == nil {
				t.Fatal("authority introduced")
			}
			if err := validateVPNConfigUpdate("client\n", "client\n"+payload+"\n", true); err == nil {
				t.Fatal("client authority introduced")
			}
		})
	}
	for _, content := range []string{strings.Replace(server, "127.0.0.1 7505", "127.0.0.1 7506", 1), strings.Replace(server, "/data/pki/ca.crt", "/etc/shadow", 1), strings.Replace(server, "script-security 3\n", "", 1), server + "client-connect /usr/bin/docker-entrypoint.sh\n"} {
		if err := validateVPNConfigUpdate(server, content, false); err == nil {
			t.Fatal("protected directive changed")
		}
	}
}

func TestClientGenerationPassesRuntimeArgumentsAndWritesValidatedCCD(t *testing.T) {
	authTestDB(t)
	configureTestAdmin(t)
	old := ovData
	ovData = t.TempDir()
	t.Cleanup(func() { ovData = old })
	bin := t.TempDir()
	capture := filepath.Join(bin, "args")
	// This inert command recorder is NOT the runtime entrypoint or installer.
	if err := os.WriteFile(filepath.Join(bin, "docker-entrypoint.sh"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$COMMAND_CAPTURE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("COMMAND_CAPTURE", capture)
	b := newBrowser(setupRouter(ovpn{}))
	loginBrowser(t, b, "admin")
	b.csrf(t)
	values := url.Values{"name": {"cert"}, "serverAddr": {"vpn.example"}, "serverPort": {"1194"}, "config": {"route 10.1.0.0 255.255.0.0"}, "ccdConfig": {"iroute 10.1.0.0 255.255.0.0"}, "mfa": {"true"}}
	if w := b.request("POST", "/ovpn/client", values); w.Code != 200 {
		t.Fatalf("generation: %d %s", w.Code, w.Body)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	want := "genclient\ncert\nvpn.example\n1194\nroute 10.1.0.0 255.255.0.0\ntrue\n"
	if string(args) != want {
		t.Errorf("runtime arguments %q want %q", args, want)
	}
	ccd, err := os.ReadFile(filepath.Join(ovData, "ccd", "cert"))
	if err != nil || string(ccd) != values.Get("ccdConfig") {
		t.Errorf("CCD %q %v", ccd, err)
	}
	os.Remove(capture)
	values.Set("name", "bad")
	values.Set("ccdConfig", "up /bin/evil")
	if w := b.request("POST", "/ovpn/client", values); w.Code != 400 {
		t.Errorf("unsafe creation %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatal("unsafe creation invoked external command")
	}
}

func TestVPNLoginWritesIdentityPolicyAndClearsStale(t *testing.T) {
	authTestDB(t)
	if err := db.AutoMigrate(&Group{}); err != nil {
		t.Fatal(err)
	}
	old := ovData
	ovData = t.TempDir()
	t.Cleanup(func() { ovData = old })
	viper.Set("system.base.max_duplicate_login", 0)
	viper.Set("system.base.validate_client_config", false)
	policy := `push "route 10.20.0.0 255.255.0.0"`
	g := Group{Name: "Default", Config: &policy}
	if err := g.Create(); err != nil {
		t.Fatal(err)
	}
	u := User{Username: "policy-user", Password: "LongPassword!123", Gid: g.ID, IpAddr: "10.8.0.9"}
	if err := u.Create(); err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte(u.Username + "\x00cert"))
	target := filepath.Join(ovData, "connections", fmt.Sprintf("%x.conf", key))
	login := func() error {
		return (&User{Username: u.Username, Password: "LongPassword!123", OvpnConfig: "cert"}).Login(true)
	}
	if err := login(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), policy) || !strings.Contains(string(b), "ifconfig-push 10.8.0.9") {
		t.Fatalf("policy missing: %q", b)
	}
	for _, name := range []string{".ovip", ".ovc"} {
		if _, err := os.Stat(filepath.Join(ovData, name)); !os.IsNotExist(err) {
			t.Fatalf("shared file %s exists", name)
		}
	}
	if err := db.Model(&u).Update("ip_addr", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&g).Update("config", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := login(); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(target)
	if err != nil || strings.TrimSpace(string(b)) != "" {
		t.Fatalf("stale policy %q: %v", b, err)
	}
	if err := db.Migrator().DropTable(&Group{}); err != nil {
		t.Fatal(err)
	}
	if err := login(); err == nil {
		t.Fatal("policy lookup failure authenticated")
	}
}
