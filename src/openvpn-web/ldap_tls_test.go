package main

import (
	"crypto/tls"
	"encoding/pem"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLDAPTLSDefaults(t *testing.T) {
	t.Setenv("LDAP_TLS_INSECURE_SKIP_VERIFY", "")
	cfg, err := ldapTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InsecureSkipVerify || cfg.MinVersion < tls.VersionTLS12 || cfg.ServerName != "localhost" {
		t.Fatalf("unsafe TLS config: %+v", cfg)
	}
	t.Setenv("LDAP_TLS_INSECURE_SKIP_VERIFY", "garbage")
	if _, err := ldapTLSConfig("localhost"); err == nil {
		t.Fatal("invalid bool accepted")
	}
}

func TestLDAPRejectsPlaintextBeforeBind(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ldapURL = "ldap://" + ln.Addr().String()
	ldapBindUserDn, ldapBindPassword = "cn=admin", "secret-never-plaintext"
	done := make(chan []byte, 1)
	go func() {
		c, e := ln.Accept()
		if e != nil {
			done <- nil
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(2 * time.Second))
		b := make([]byte, 4096)
		n, _ := c.Read(b)
		done <- b[:n]
	}()
	if _, err := InitLdap(); err == nil {
		t.Fatal("plaintext server accepted")
	}
	b := <-done
	// The only first request allowed is StartTLS extended operation, never Bind.
	if !containsBytes(b, []byte("1.3.6.1.4.1.1466.20037")) || containsBytes(b, []byte(ldapBindPassword)) {
		t.Fatalf("unsafe LDAP first packet: %x", b)
	}
}
func TestLDAPCertificateTrust(t *testing.T) {
	t.Setenv("LDAP_TLS_INSECURE_SKIP_VERIFY", "")
	t.Setenv("LDAP_CA_FILE", "")
	t.Setenv("LDAP_CA_DIR", "")
	server := httptest.NewUnstartedServer(nil)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	address := server.Listener.Addr().String()
	dial := func(host string) error {
		cfg, err := ldapTLSConfig(host)
		if err != nil {
			return err
		}
		c, err := tls.Dial("tcp", address, cfg)
		if c != nil {
			c.Close()
		}
		return err
	}
	if err := dial("example.com"); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LDAP_CA_FILE", ca)
	if err := dial("example.com"); err != nil {
		t.Fatalf("trusted CA rejected: %v", err)
	}
	if err := dial("wrong.invalid"); err == nil {
		t.Fatal("hostname mismatch accepted")
	}
	t.Setenv("LDAP_CA_FILE", "")
	t.Setenv("LDAP_CA_DIR", filepath.Dir(ca))
	if err := dial("example.com"); err != nil {
		t.Fatalf("CA directory rejected: %v", err)
	}
	var warning strings.Builder
	old := log.Writer()
	log.SetOutput(&warning)
	defer log.SetOutput(old)
	t.Setenv("LDAP_TLS_INSECURE_SKIP_VERIFY", "true")
	if err := dial("wrong.invalid"); err != nil {
		t.Fatalf("explicit override failed: %v", err)
	}
	if !strings.Contains(warning.String(), "WARNING") {
		t.Fatal("missing insecure TLS warning")
	}
	t.Setenv("LDAP_CA_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := ldapTLSConfig("example.com"); err == nil {
		t.Fatal("missing CA accepted")
	}
}

func containsBytes(a, b []byte) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if string(a[i:i+len(b)]) == string(b) {
			return true
		}
	}
	return false
}
