package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func runtimeBrowser(t *testing.T, ov ovpn) *browser {
	t.Helper()
	authTestDB(t)
	configureTestAdmin(t)
	old := ovData
	ovData = t.TempDir()
	t.Cleanup(func() { ovData = old })
	b := newBrowser(setupRouter(ov))
	loginBrowser(t, b, "admin")
	b.csrf(t)
	return b
}

func TestKillRouteRejectsNonNumericCID(t *testing.T) {
	b := runtimeBrowser(t, ovpn{})
	for _, cid := range []string{"", "1\nsignal SIGTERM", "1 2", "-1", "+1", "１", "18446744073709551616"} {
		if w := b.request("POST", "/ovpn/kill", url.Values{"cid": {cid}}); w.Code != 400 {
			t.Errorf("cid %q returned %d", cid, w.Code)
		}
	}
	if w := b.request("POST", "/ovpn/kill", url.Values{"cid": {"1"}}); w.Code != 502 {
		t.Errorf("management failure returned %d", w.Code)
	}
}

func TestKillRouteManagementResponses(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
	}{{"success", "SUCCESS: client killed\r\n", 200}, {"error", "ERROR: client not found\r\n", 502}, {"disconnect", "", 502}, {"incomplete", "SUCCESS", 502}} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			commands := make(chan string, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					commands <- "accept failed"
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				line, _ := bufio.NewReader(conn).ReadString('\n')
				commands <- line
				conn.Write([]byte(tc.response))
			}()
			b := runtimeBrowser(t, ovpn{address: listener.Addr().String()})
			w := b.request("POST", "/ovpn/kill", url.Values{"cid": {"42"}})
			if w.Code != tc.status {
				t.Errorf("got %d want %d: %s", w.Code, tc.status, w.Body)
			}
			if command := <-commands; command != "client-kill 42 HALT\n" {
				t.Errorf("wire command %q", command)
			}
		})
	}
}

func TestCCDRoutePrivateAtomicReplacement(t *testing.T) {
	b := runtimeBrowser(t, ovpn{})
	dir := filepath.Join(ovData, "ccd")
	os.MkdirAll(dir, 0755)
	target := filepath.Join(dir, "cert")
	os.WriteFile(target, []byte("old"), 0644)
	os.Link(target, filepath.Join(dir, "old-link"))
	os.WriteFile(filepath.Join(ovData, "server.conf"), []byte("client-config-dir "+dir+"\n"), 0600)
	w := b.request("PUT", "/ovpn/client/cert/ccd", url.Values{"content": {"iroute 10.0.0.0 255.0.0.0\n"}})
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for p, want := range map[string]os.FileMode{dir: 0700, target: 0600} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("mode %s: %v %v", p, info, err)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "old-link")); string(data) != "old" {
		t.Error("CCD inode overwritten in place")
	}
}

func TestMFAProfileEditorAllowsOnlyFixedChallenge(t *testing.T) {
	original := "client\nstatic-challenge \"Enter MFA code\" 1\nverb 2\n"
	if err := validateVPNConfigUpdate(original, original+"route 10.0.0.0 255.0.0.0\n", true); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"static-challenge \"other\" 1", "static-challenge \"Enter MFA code\" 0", "static-challenge \"Enter MFA code\" 1 extra"} {
		if err := validateVPNConfigUpdate("client\n", "client\n"+line+"\n", true); err == nil {
			t.Errorf("accepted %s", line)
		}
	}
}

func TestSettingsRouteReportsOpenVPNPersistenceFailure(t *testing.T) {
	b := runtimeBrowser(t, ovpn{})
	// Missing server.conf prevents generation: settings must not claim success.
	if w := b.request("POST", "/settings", url.Values{"currentPass": {"LongPassword!123"}, "system.base.auto_update_ovpn_config": {"true"}, "openvpn.ovpn_port": {"1195"}}); w.Code != 500 {
		t.Fatalf("unpersisted OpenVPN settings returned %d %s", w.Code, w.Body)
	}
	if w := b.request("POST", "/settings", url.Values{"currentPass": {"LongPassword!123"}, "system.base.max_duplicate_login": {"2"}}); w.Code != 500 {
		t.Fatalf("unpersisted status settings returned %d %s", w.Code, w.Body)
	}
}

func TestSettingsRouteAtomicPrivatePersistence(t *testing.T) {
	b := runtimeBrowser(t, ovpn{})
	target := viper.ConfigFileUsed()
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	oldLink := filepath.Join(t.TempDir(), "original.json")
	if err := os.Link(target, oldLink); err != nil {
		t.Fatal(err)
	}
	values := url.Values{"currentPass": {"LongPassword!123"}, "system.base.auto_update_ovpn_config": {"false"}, "openvpn.ovpn_port": {"1196"}}
	if w := b.request("POST", "/settings", values); w.Code != 200 {
		t.Fatalf("settings write %d %s", w.Code, w.Body)
	}
	if data, err := os.ReadFile(oldLink); err != nil || string(data) != string(original) {
		t.Fatalf("settings written in-place: %v", err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("configuration mode: %v %v", info, err)
	}
	// A rename onto an existing directory is a deterministic publication failure,
	// including when tests run as root and permission failures cannot be induced.
	viper.SetConfigFile(t.TempDir())
	values.Set("openvpn.ovpn_port", "1197")
	if w := b.request("POST", "/settings", values); w.Code != 500 {
		t.Fatalf("failed persistence %d %s", w.Code, w.Body)
	}
	if got := viper.GetString("openvpn.ovpn_port"); got != "1196" {
		t.Fatalf("failed write published live state %q", got)
	}
}

func TestVPNConfigSavePrivateAtomic(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "server.conf")
	os.WriteFile(target, []byte("old"), 0644)
	os.Link(target, filepath.Join(dir, "old-link"))
	cfg := VPNConfig{ConfigPath: target, Lines: []string{"port 1195"}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	cfg.ConfigPath = dir
	if err := cfg.Save(); err == nil {
		t.Fatal("rename failure swallowed")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".config-*")); len(matches) != 0 {
		t.Errorf("temporary files retained: %v", matches)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0600 {
		t.Error("configuration not private")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "old-link")); string(data) != "old" {
		t.Error("configuration overwritten in place")
	}
}

func TestRevokeRouteCRLFailurePreservesFilesAndRetries(t *testing.T) {
	b := runtimeBrowser(t, ovpn{})
	pki := filepath.Join(ovData, "pki")
	for _, d := range []string{"pki/issued", "clients", "ccd", "bin"} {
		if err := os.MkdirAll(filepath.Join(ovData, d), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(ovData, name), data, 0700); err != nil {
			t.Fatal(err)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture-ca"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	write("pki/ca.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	cert := &x509.Certificate{SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "cert"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	certDER, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	write("pki/issued/cert.crt", certPEM)
	profile := "client\n<cert>\n" + string(certPEM) + "</cert>\n"
	write("clients/cert.ovpn", []byte(profile))
	write("ccd/cert", []byte("iroute 10.0.0.0 255.0.0.0\n"))
	write("server.conf", []byte("crl-verify "+filepath.Join(pki, "crl.pem")+"\n"))
	crlDER, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{Number: big.NewInt(1), ThisUpdate: time.Now().Add(-time.Minute), NextUpdate: time.Now().Add(time.Hour), RevokedCertificateEntries: []x509.RevocationListEntry{{SerialNumber: big.NewInt(42), RevocationTime: time.Now().Add(-time.Minute)}}}, ca, key)
	if err != nil {
		t.Fatal(err)
	}
	write("pki/ready.pem", pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crlDER}))
	write("pki/index.txt", []byte("V\t300101000000Z\t\t2A\tunknown\t/CN=cert\n"))
	// Inert EasyRSA replacement: records argv and models an irreversible index change.
	write("bin/easyrsa", []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$EASYRSA_PKI/calls\"\nif [ \"$1\" = '--batch' ]; then\n printf 'R\\t300101000000Z\\t261024000000Z\\t2A\\tunknown\\t/CN=cert\\n' > \"$EASYRSA_PKI/index.txt\"\n exit 0\nfi\nif [ -e \"$EASYRSA_PKI/fail\" ]; then exit 1; fi\nif [ -e \"$EASYRSA_PKI/omit\" ]; then exit 0; fi\n/bin/cp \"$EASYRSA_PKI/ready.pem\" \"$EASYRSA_PKI/crl.pem\"\n"))
	t.Setenv("PATH", filepath.Join(ovData, "bin"))
	t.Setenv("EASYRSA_PKI", pki)
	write("pki/fail", nil)
	w := b.request("DELETE", "/ovpn/client/cert", nil)
	if w.Code != 500 || strings.Contains(w.Body.String(), "删除客户端成功") {
		t.Errorf("CRL failure response %d %s", w.Code, w.Body)
	}
	for _, f := range []string{"clients/cert.ovpn", "ccd/cert"} {
		if _, err := os.Stat(filepath.Join(ovData, f)); err != nil {
			t.Errorf("CRL failure removed %s", f)
		}
	}
	os.Remove(filepath.Join(pki, "fail"))
	write("pki/omit", nil)
	if w := b.request("DELETE", "/ovpn/client/cert", nil); w.Code != 500 {
		t.Errorf("missing effective CRL returned %d", w.Code)
	}
	os.Remove(filepath.Join(pki, "omit"))
	for _, tc := range []struct {
		name    string
		serial  int64
		next    time.Time
		corrupt bool
	}{
		{"wrong-serial", 43, time.Now().Add(time.Hour), false},
		{"expired", 42, time.Now().Add(-time.Minute), false},
		{"bad-signature", 42, time.Now().Add(time.Hour), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			der, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{Number: big.NewInt(2), ThisUpdate: time.Now().Add(-time.Hour), NextUpdate: tc.next, RevokedCertificateEntries: []x509.RevocationListEntry{{SerialNumber: big.NewInt(tc.serial), RevocationTime: time.Now().Add(-time.Hour)}}}, ca, key)
			if err != nil {
				t.Fatal(err)
			}
			if tc.corrupt {
				der[len(der)-1] ^= 1
			}
			write("pki/ready.pem", pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: der}))
			if w := b.request("DELETE", "/ovpn/client/cert", nil); w.Code != 500 {
				t.Errorf("invalid effective CRL returned %d %s", w.Code, w.Body)
			}
			for _, f := range []string{"clients/cert.ovpn", "ccd/cert"} {
				if _, err := os.Stat(filepath.Join(ovData, f)); err != nil {
					t.Errorf("invalid CRL removed %s", f)
				}
			}
		})
	}
	write("pki/ready.pem", pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crlDER}))
	// EasyRSA can move the issued certificate on revoke; the retained profile supplies it on retry.
	os.Remove(filepath.Join(pki, "issued/cert.crt"))
	if w := b.request("DELETE", "/ovpn/client/cert", nil); w.Code != 200 {
		t.Fatalf("retry failed %d %s", w.Code, w.Body)
	}
	for _, f := range []string{"clients/cert.ovpn", "ccd/cert"} {
		if _, err := os.Stat(filepath.Join(ovData, f)); !os.IsNotExist(err) {
			t.Errorf("successful revoke retained %s: %v", f, err)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(pki, "calls"))
	if strings.Count(string(calls), "--batch revoke cert") != 1 {
		t.Errorf("retry reran revoke: %s", calls)
	}
}
