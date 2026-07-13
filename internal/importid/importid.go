// Package importid holds the content-addressed Terraform id grammar for DNS
// records, in one place so the resource ImportState parsers, the resource id
// builders, and the tf-dns-import generator can never drift apart.
//
// The SpaceWeb API addresses records by a per-type index that shifts as the zone
// changes, so it is never a stable id. A record is identified by its content
// instead:
//
//	A/AAAA/CNAME/MX/TXT/NS -> "<domain>/<type>/<host>/<value>"
//	SRV                    -> "<domain>/<service>/<protocol>/<host>/<target>/<port>"
//
// where <host> is the wire `domain` field for TXT and the `name` field for every
// other type, with the apex ("@") normalized to "".
package importid

import (
	"strconv"
	"strings"
)

// Apex normalizes the zone apex host ("@") to the empty string.
func Apex(host string) string {
	if host == "@" {
		return ""
	}
	return host
}

// Host returns a record's host label from the two host-bearing wire fields: TXT
// carries the host in `domain`, every other type in `name`. The apex normalizes
// to "".
func Host(recType, name, domain string) string {
	if strings.EqualFold(recType, "TXT") {
		return Apex(domain)
	}
	return Apex(name)
}

// Record builds the id for A/AAAA/CNAME/MX/TXT/NS. The value may contain slashes
// (e.g. a TXT with base64) — it is the last, unsplit segment on the parsing side.
func Record(domain, recType, host, value string) string {
	return strings.Join([]string{domain, recType, host, value}, "/")
}

// SRV builds the id for an SRV record.
func SRV(domain, service, protocol, host, target string, port int) string {
	return strings.Join([]string{
		domain, service, protocol, host, target, strconv.Itoa(port),
	}, "/")
}
