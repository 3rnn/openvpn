package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/spf13/viper"
)

type LdapUserData struct {
	Username   string `json:"username"`
	LdapAuth   bool   `json:"ldapAuth"`
	Ipaddr     string `json:"ipaddr"`
	OvpnConfig string `json:"ovpnConfig"`
}

type LdapConn struct {
	Conn *ldap.Conn
}

// ldapTLSConfig extends system roots with explicitly mounted PEM trust anchors.
// A malformed override fails closed, rather than silently disabling verification.
func ldapTLSConfig(host string) (*tls.Config, error) {
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if raw := os.Getenv("LDAP_TLS_INSECURE_SKIP_VERIFY"); raw != "" {
		insecure, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid LDAP_TLS_INSECURE_SKIP_VERIFY: %w", err)
		}
		cfg.InsecureSkipVerify = insecure
		if insecure {
			log.Print("WARNING: LDAP TLS certificate verification DISABLED by LDAP_TLS_INSECURE_SKIP_VERIFY; credentials are vulnerable to interception")
		}
	}
	caFile, caDir := os.Getenv("LDAP_CA_FILE"), os.Getenv("LDAP_CA_DIR")
	if caFile != "" || caDir != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		appendFile := func(name string) error {
			pem, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			if !roots.AppendCertsFromPEM(pem) {
				return fmt.Errorf("no PEM certificates in %s", name)
			}
			return nil
		}
		if caFile != "" {
			if err := appendFile(caFile); err != nil {
				return nil, err
			}
		}
		if caDir != "" {
			entries, err := os.ReadDir(caDir)
			if err != nil {
				return nil, err
			}
			count := 0
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if err := appendFile(filepath.Join(caDir, entry.Name())); err != nil {
					return nil, err
				}
				count++
			}
			if count == 0 {
				return nil, fmt.Errorf("LDAP_CA_DIR contains no certificates")
			}
		}
		cfg.RootCAs = roots
	}
	return cfg, nil
}

func InitLdap() (*LdapConn, error) {
	u, err := url.Parse(ldapURL)
	if err != nil || (u.Scheme != "ldaps" && u.Scheme != "ldap") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("LDAP URL must be ldap:// or ldaps:// with a hostname")
	}
	cfg, err := ldapTLSConfig(u.Hostname())
	if err != nil {
		return nil, err
	}
	l, err := ldap.DialURL(ldapURL, ldap.DialWithTLSConfig(cfg), ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}))
	if err != nil {
		return nil, fmt.Errorf("ldap connect error: %w", err)
	}
	l.SetTimeout(5 * time.Second)
	if u.Scheme == "ldap" {
		if err := l.StartTLS(cfg); err != nil {
			l.Close()
			return nil, fmt.Errorf("mandatory LDAP StartTLS failed: %w", err)
		}
	}
	if err := l.Bind(ldapBindUserDn, ldapBindPassword); err != nil {
		l.Close()
		return nil, fmt.Errorf("ldap bind error: %w", err)
	}
	return &LdapConn{Conn: l}, nil
}

func (l *LdapConn) LdapSearch(username string) (*ldap.SearchResult, error) {
	f := fmt.Sprintf("(%s=%s)", ldapUserAttribute, ldap.EscapeFilter(username))
	if ldapUserGroupFilter {
		f = fmt.Sprintf("(&(%s=%s)(memberOf=%s))", ldapUserAttribute, ldap.EscapeFilter(username), ldap.EscapeFilter(ldapUserGroupDn))
	}

	searchRequest := ldap.NewSearchRequest(
		ldapBaseDn,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		f,
		[]string{"dn", ldapUserAttrIpaddrName, ldapUserAttrConfigName},
		nil,
	)

	sr, err := l.Conn.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	if len(sr.Entries) == 0 {
		return nil, fmt.Errorf("用户不存在")
	}

	if len(sr.Entries) > 1 {
		return nil, fmt.Errorf("用户存在多个条目")
	}

	return sr, nil
}

func (l *LdapConn) Auth(clogin bool, username, password, commonName string) error {
	defer l.Conn.Close()
	sr, err := l.LdapSearch(username)
	if err != nil {
		return err
	}

	userdn := sr.Entries[0].DN

	err = l.Conn.Bind(userdn, password)
	if err != nil {
		return err
	}

	if clogin {
		if viper.GetBool("system.base.validate_client_config") {
			configName := sr.Entries[0].GetAttributeValue(ldapUserAttrConfigName)
			if commonName != strings.TrimSuffix(configName, ".ovpn") {
				return fmt.Errorf("使用非法配置文件登录")
			}
		}

		ipaddr := sr.Entries[0].GetAttributeValue(ldapUserAttrIpaddrName)
		if err := writeClientConnectionConfig(username, commonName, ipaddr, ""); err != nil {
			return err
		}
	}

	return nil
}

func (l *LdapConn) Get(username string) (LdapUserData, error) {
	defer l.Conn.Close()
	sr, err := l.LdapSearch(username)
	if err != nil {
		return LdapUserData{}, err
	}

	return LdapUserData{
		Username:   username,
		LdapAuth:   ldapAuth,
		Ipaddr:     sr.Entries[0].GetAttributeValue(ldapUserAttrIpaddrName),
		OvpnConfig: sr.Entries[0].GetAttributeValue(ldapUserAttrConfigName),
	}, nil
}
