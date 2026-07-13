package main

import (
	"strings"
	"testing"

	"github.com/sanchpet/sweb-go-sdk/dns"
)

// TestBlocks pins the import-block output for the tricky record shapes: apex,
// wildcard, round-robin (same host, distinct values -> distinct labels & ids),
// TXT host in the `domain` field, a base64 TXT value, and SRV.
func TestBlocks(t *testing.T) {
	recs := []dns.Record{
		{Type: "A", Name: "", Value: "185.158.115.81"},
		{Type: "A", Name: "*.infra", Value: "168.222.202.148"},
		{Type: "A", Name: "*.infra", Value: "77.222.52.113"},
		{Type: "MX", Name: "", Value: "mx1.spaceweb.ru.", Priority: 10},
		{Type: "TXT", Domain: "@", Value: "v=spf1 -all"},
		{Type: "TXT", Domain: "sweb._domainkey", Value: "v=DKIM1; p=AB/CD+ef"},
		{Type: "SRV", Name: "", Service: "autodiscover", Protocol: "tcp", Target: "autodiscover.spaceweb.ru.", Port: 443, Priority: 5},
	}
	got := strings.Join(blocks("sanch.pet", recs), "")

	wantIDs := []string{
		`id = "sanch.pet/A//185.158.115.81"`,
		`id = "sanch.pet/A/*.infra/168.222.202.148"`,
		`id = "sanch.pet/A/*.infra/77.222.52.113"`,
		`id = "sanch.pet/MX//mx1.spaceweb.ru."`,
		`id = "sanch.pet/TXT//v=spf1 -all"`,                        // apex TXT: "@" domain -> ""
		`id = "sanch.pet/TXT/sweb._domainkey/v=DKIM1; p=AB/CD+ef"`, // host from domain; value keeps its slash
		`id = "sanch.pet/autodiscover/tcp//autodiscover.spaceweb.ru./443"`,
	}
	for _, w := range wantIDs {
		if !strings.Contains(got, w) {
			t.Errorf("missing import id:\n  want %s\n  in:\n%s", w, got)
		}
	}

	// Round-robin: the two *.infra A records must produce distinct labels.
	if strings.Count(got, "to = sweb_dns_record.a_infra_") != 2 {
		t.Errorf("round-robin labels not disambiguated:\n%s", got)
	}
	// SRV routes to the SRV resource type.
	if !strings.Contains(got, "to = sweb_dns_srv_record.") {
		t.Errorf("SRV not routed to sweb_dns_srv_record:\n%s", got)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"A":     "a",
		"*":     "rec", // all-punct collapses to the fallback
		"WWW-1": "www_1",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
