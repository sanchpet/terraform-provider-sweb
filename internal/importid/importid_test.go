package importid

import "testing"

func TestHost(t *testing.T) {
	cases := []struct {
		recType, name, domain, want string
	}{
		{"A", "www", "", "www"},
		{"A", "", "", ""},                                 // apex
		{"CNAME", "@", "", ""},                            // "@" normalized
		{"TXT", "", "@", ""},                              // TXT apex host lives in domain
		{"TXT", "", "sweb._domainkey", "sweb._domainkey"}, // TXT host from domain
		{"txt", "", "dkim", "dkim"},                       // type match is case-insensitive
	}
	for _, c := range cases {
		if got := Host(c.recType, c.name, c.domain); got != c.want {
			t.Errorf("Host(%q,%q,%q) = %q, want %q", c.recType, c.name, c.domain, got, c.want)
		}
	}
}

func TestRecordAndSRV(t *testing.T) {
	if got := Record("d", "A", "", "1.2.3.4"); got != "d/A//1.2.3.4" {
		t.Errorf("Record apex = %q", got)
	}
	if got := Record("d", "TXT", "k", "a/b+c"); got != "d/TXT/k/a/b+c" {
		t.Errorf("Record with slash in value = %q", got)
	}
	if got := SRV("d", "sip", "tcp", "", "h.", 5060); got != "d/sip/tcp//h./5060" {
		t.Errorf("SRV = %q", got)
	}
}
