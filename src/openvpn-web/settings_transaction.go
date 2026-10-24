package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// prepareSettingsVPN is deliberately pure: no firewall commands, reloads, live
// globals or writes while any document is still being validated.
func prepareSettingsVPN(v *viper.Viper) (*VPNConfig, bool, error) {
	auto := v.GetBool("system.base.auto_update_ovpn_config")
	status := v.GetInt("system.base.max_duplicate_login") > 0
	if !auto && !status {
		return nil, false, nil
	}
	cfg, err := initOvpnConfig()
	if err != nil {
		return nil, false, err
	}
	reload := false
	if status {
		value := filepath.Join(ovData, "openvpn-status.log") + " 1"
		reload = cfg.Get("status") != value || cfg.Get("status-version") != "3"
		cfg.Set("status", value)
		cfg.Set("status-version", "3")
	}
	if auto {
		for k, val := range v.GetStringMap("openvpn") {
			if err := validateVPNSetting("openvpn."+k, fmt.Sprint(val)); err != nil {
				return nil, false, err
			}
		}
		for key, directive := range map[string]string{"ovpn_port": "port", "ovpn_proto": "proto", "ovpn_max_clients": "max-clients"} {
			if v.IsSet("openvpn." + key) {
				cfg.Set(directive, v.GetString("openvpn."+key))
			}
		}
		if value := v.GetString("openvpn.ovpn_subnet"); value != "" {
			ip, network, err := net.ParseCIDR(value)
			if err != nil {
				return nil, false, err
			}
			cfg.Set("server", fmt.Sprintf("%s %s", ip, net.IP(network.Mask)))
		}
		proto := v.GetString("openvpn.ovpn_proto")
		if v.GetBool("openvpn.ovpn_ipv6") {
			subnet := v.GetString("openvpn.ovpn_subnet6")
			if _, _, err := net.ParseCIDR(subnet); err != nil {
				return nil, false, err
			}
			if !strings.HasSuffix(proto, "6") {
				proto += "6"
			}
			cfg.Set("server-ipv6", subnet)
		} else {
			cfg.Delete("server-ipv6")
		}
		if proto != "" {
			cfg.Set("proto", proto)
		}
		if value := v.GetString("openvpn.ovpn_management"); value != "" {
			cfg.Set("management", strings.ReplaceAll(value, ":", " "))
		}
		var dns []int
		for i, line := range cfg.Lines {
			if strings.Contains(line, "dhcp-option DNS") {
				dns = append(dns, i)
			}
		}
		cfg.DeleteLines(dns)
		cfg.Delete(`push "redirect-gateway def1 ipv6 bypass-dhcp"`)
		if v.GetBool("openvpn.ovpn_gateway") {
			for _, key := range []string{"ovpn_push_dns1", "ovpn_push_dns2"} {
				value := v.GetString("openvpn." + key)
				if net.ParseIP(value) == nil {
					return nil, false, fmt.Errorf("invalid DNS address")
				}
				cfg.Set("push", fmt.Sprintf(`"dhcp-option DNS %s"`, value))
			}
			cfg.Set("push", `"redirect-gateway def1 ipv6 bypass-dhcp"`)
		}
		port := v.GetString("system.base.web_port")
		cfg.Set("setenv auth_api", fmt.Sprintf("http://127.0.0.1:%s/login", port))
		cfg.Set("setenv ovpn_auth_api", fmt.Sprintf("http://127.0.0.1:%s/ovpn/login", port))
		cfg.Set("setenv ovpn_history_api", fmt.Sprintf("http://127.0.0.1:%s/ovpn/history", port))
	}
	return cfg, reload, nil
}

func publishSettings(v *viper.Viper) (bool, error) {
	return publishSettingsWithWriter(v, atomicPrivateConfig)
}

// Both complete documents are prepared first. Live credentials are installed
// only after both durable publications succeed. A failed publication can already
// have renamed its target (directory-sync failure), so inspect/restore ALL targets.
// This is a checked runtime rollback, not a multi-file crash-atomic transaction.
// A crash can leave private .settings-recovery-* snapshots; compare/restore both
// documents offline before restarting if publication was interrupted.
func publishSettingsWithWriter(v *viper.Viper, write func(string, []byte) error) (bool, error) {
	var decoded config
	if err := v.Unmarshal(&decoded); err != nil {
		return false, fmt.Errorf("settings validation failed")
	}
	cfg, reload, err := prepareSettingsVPN(v)
	if err != nil {
		return false, fmt.Errorf("OpenVPN configuration preparation failed")
	}
	content, err := json.MarshalIndent(v.AllSettings(), "", "  ")
	if err != nil {
		return false, fmt.Errorf("settings serialization failed")
	}
	targets := []string{v.ConfigFileUsed()}
	documents := [][]byte{append(content, '\n')}
	if cfg != nil {
		targets = append(targets, cfg.ConfigPath)
		documents = append(documents, []byte(strings.Join(cfg.Lines, "\n")+"\n"))
	}
	originals := make([][]byte, len(targets))
	backups := make([]string, 0, len(targets))
	keep := false
	defer func() {
		if !keep {
			for _, p := range backups {
				os.Remove(p)
			}
		}
	}()
	for i, target := range targets {
		originals[i], err = os.ReadFile(target)
		if err != nil {
			return false, fmt.Errorf("settings snapshot failed")
		}
		f, e := os.CreateTemp(filepath.Dir(target), ".settings-recovery-*")
		if e != nil {
			return false, fmt.Errorf("settings recovery snapshot failed")
		}
		backups = append(backups, f.Name())
		if e = f.Close(); e != nil {
			return false, e
		}
		if e = atomicPrivateConfig(f.Name(), originals[i]); e != nil {
			return false, fmt.Errorf("settings recovery snapshot failed")
		}
	}
	for i, target := range targets {
		if err = write(target, documents[i]); err != nil {
			var rollback []error
			for j, p := range targets {
				got, e := os.ReadFile(p)
				if j > i && e == nil && bytes.Equal(got, originals[j]) {
					continue
				}
				if e = write(p, originals[j]); e != nil {
					rollback = append(rollback, e)
				}
				got, e = os.ReadFile(p)
				if e != nil || !bytes.Equal(got, originals[j]) {
					rollback = append(rollback, fmt.Errorf("restore verification failed"))
				}
			}
			if len(rollback) > 0 {
				keep = true
				return false, fmt.Errorf("settings publication failed; rollback incomplete; recovery required: stop service, restore private snapshots %s to %s respectively, then restart: %w", strings.Join(backups, ", "), strings.Join(targets, ", "), errors.Join(rollback...))
			}
			return false, fmt.Errorf("settings publication failed; original files verified restored")
		}
	}
	for _, key := range v.AllKeys() {
		viper.Set(key, v.Get(key))
	}
	loadConfig()
	// Retain existing network reconciliation, strictly after document commit.
	// The old document supplies old network ranges for rule removal.
	if cfg != nil {
		cfg = &VPNConfig{ConfigPath: cfg.ConfigPath, Lines: strings.Split(strings.TrimSuffix(string(originals[1]), "\n"), "\n")}
	}
	if cfg != nil && v.GetBool("system.base.auto_update_ovpn_config") {
		for _, key := range []string{"ovpn_subnet", "ovpn_ipv6", "ovpn_subnet6"} {
			if v.IsSet("openvpn." + key) {
				cfg.Update("openvpn."+key, v.GetString("openvpn."+key))
			}
		}
	}
	return reload, nil
}
