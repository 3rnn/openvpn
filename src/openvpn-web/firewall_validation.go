package main

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

var nftIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

func validateNFTNames(names ...string) error {
	for _, name := range names {
		if !nftIdentifier.MatchString(name) {
			return fmt.Errorf("invalid nft identifier")
		}
	}
	if !nftIdentifier.MatchString(nftTableName) {
		return fmt.Errorf("invalid nft table")
	}
	return nil
}
func validateNFTAddresses(ips string) error {
	for _, s := range strings.Split(ips, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if a, err := netip.ParseAddr(s); err == nil && a.Zone() == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil && p.Addr().Zone() == "" {
			continue
		}
		if parts := strings.Split(s, "-"); len(parts) == 2 {
			a, e1 := netip.ParseAddr(parts[0])
			b, e2 := netip.ParseAddr(parts[1])
			if e1 == nil && e2 == nil && a.Zone() == "" && b.Zone() == "" && a.BitLen() == b.BitLen() && a.Compare(b) <= 0 {
				continue
			}
		}
		return fmt.Errorf("invalid IP address, prefix or range")
	}
	return nil
}
func validateNFTChain(chain string) error {
	if chain != "forward" && chain != "upload" && chain != "download" {
		return fmt.Errorf("invalid nft chain")
	}
	return validateNFTNames(chain)
}
func validateNFTPolicy(policy string) error {
	switch policy {
	case "accept", "drop", "reject", "continue":
		return nil
	}
	return fmt.Errorf("invalid firewall policy")
}
