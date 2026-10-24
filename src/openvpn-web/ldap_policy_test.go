package main

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
	"github.com/spf13/viper"
)

// Exercise the actual LDAP search/bind client against an in-memory protocol peer;
// no directory credentials, listener, external directory or runtime command is used.
func ldapPolicyPeer(t *testing.T, ip string, bindOK bool) *LdapConn {
	t.Helper()
	client, server := net.Pipe()
	server.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan error, 1)
	go func() {
		defer server.Close()
		send := func(id interface{}, op *ber.Packet) error {
			p := ber.NewSequence("")
			p.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, ""))
			p.AppendChild(op)
			_, err := server.Write(p.Bytes())
			return err
		}
		result := func(tag ber.Tag, code int) *ber.Packet {
			p := ber.Encode(ber.ClassApplication, ber.TypeConstructed, tag, nil, "")
			p.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, code, ""))
			p.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
			p.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
			return p
		}
		req, err := ber.ReadPacket(server)
		if err != nil {
			done <- err
			return
		}
		if req.Children[1].Tag != 3 {
			done <- fmt.Errorf("expected LDAP search")
			return
		}
		entry := ber.Encode(ber.ClassApplication, ber.TypeConstructed, 4, nil, "")
		entry.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "cn=alice,dc=test", ""))
		attrs := ber.NewSequence("")
		for name, value := range map[string]string{ldapUserAttrIpaddrName: ip, ldapUserAttrConfigName: "cert.ovpn"} {
			attr := ber.NewSequence("")
			attr.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, name, ""))
			values := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "")
			values.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, value, ""))
			attr.AppendChild(values)
			attrs.AppendChild(attr)
		}
		entry.AppendChild(attrs)
		if err = send(req.Children[0].Value, entry); err != nil {
			done <- err
			return
		}
		if err = send(req.Children[0].Value, result(5, 0)); err != nil {
			done <- err
			return
		}
		req, err = ber.ReadPacket(server)
		if err != nil {
			done <- err
			return
		}
		if req.Children[1].Tag != 0 {
			done <- fmt.Errorf("expected LDAP bind")
			return
		}
		code := 0
		if !bindOK {
			code = 49
		}
		done <- send(req.Children[0].Value, result(1, code))
	}()
	conn := ldap.NewConn(client, true)
	conn.Start()
	t.Cleanup(func() {
		conn.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return &LdapConn{Conn: conn}
}

func TestLDAPConnectionPolicyResetsAndFailsClosed(t *testing.T) {
	old := ovData
	ovData = t.TempDir()
	t.Cleanup(func() { ovData = old })
	oldIP, oldConfig, oldUser := ldapUserAttrIpaddrName, ldapUserAttrConfigName, ldapUserAttribute
	ldapUserAttrIpaddrName, ldapUserAttrConfigName, ldapUserAttribute = "testIP", "testConfig", "uid"
	t.Cleanup(func() { ldapUserAttrIpaddrName, ldapUserAttrConfigName, ldapUserAttribute = oldIP, oldConfig, oldUser })
	prior := viper.Get("system.base.validate_client_config")
	viper.Set("system.base.validate_client_config", true)
	t.Cleanup(func() { viper.Set("system.base.validate_client_config", prior) })
	key := sha256.Sum256([]byte("alice\x00cert"))
	target := filepath.Join(ovData, "connections", fmt.Sprintf("%x.conf", key))
	for _, ip := range []string{"10.8.0.19", ""} {
		if err := ldapPolicyPeer(t, ip, true).Auth(true, "alice", "fixture-password", "cert"); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if ip == "" && strings.TrimSpace(string(b)) != "" {
			t.Fatalf("stale LDAP policy: %q", b)
		}
		if ip != "" && !strings.Contains(string(b), "ifconfig-push "+ip) {
			t.Fatalf("missing LDAP IP: %q", b)
		}
	}
	if err := ldapPolicyPeer(t, "10.8.0.99", false).Auth(true, "alice", "bad-password", "cert"); err == nil {
		t.Fatal("failed LDAP bind authenticated")
	}
	b, err := os.ReadFile(target)
	if err != nil || strings.TrimSpace(string(b)) != "" {
		t.Fatalf("failed LDAP bind changed policy: %q %v", b, err)
	}
	if err := ldapPolicyPeer(t, "10.8.0.99", true).Auth(true, "alice", "fixture-password", "wrong-cert"); err == nil {
		t.Fatal("wrong LDAP certificate authenticated")
	}
	if err := ldapPolicyPeer(t, "bad-IP", true).Auth(true, "alice", "fixture-password", "cert"); err == nil {
		t.Fatal("invalid LDAP policy authenticated")
	}
}
