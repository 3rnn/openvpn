package main

import "testing"

func TestNFTRejectsGrammarInjection(t *testing.T) {
	for _, ips := range []string{"1.2.3.4 }; flush ruleset; #", "::1\nflush ruleset", "10.0.0.1-::1", "10.0.0.9-10.0.0.1", "garbage"} {
		if err := setNftBlackList(ips, "add"); err == nil {
			t.Errorf("accepted address %q", ips)
		}
	}
	if err := setNftBlackList("", "add element; flush ruleset"); err == nil {
		t.Fatal("accepted action injection")
	}
	if err := setNftQosChain("upload", "1.2.3.4", "1;flush ruleset", "kbytes/second"); err == nil {
		t.Fatal("accepted rate injection")
	}
	if err := setNftChain("forward", Firewall{Policy: "accept;flush ruleset"}); err == nil {
		t.Fatal("accepted policy injection")
	}
	if err := setNftTableSet("bad; flush ruleset", ""); err == nil {
		t.Fatal("accepted name injection")
	}
}
func TestNFTAddressGrammar(t *testing.T) {
	for _, ip := range []string{"10.0.0.1", "10.0.0.0/24", "10.0.0.1-10.0.0.9", "::1", "2001:db8::/32"} {
		if err := validateNFTAddresses(ip); err != nil {
			t.Errorf("%s: %v", ip, err)
		}
	}
}
