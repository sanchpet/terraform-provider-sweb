// Command tf-dns-import turns a SpaceWeb DNS zone dump into Terraform import{}
// blocks for the sweb provider, so an existing zone can be adopted into
// declarative config without hand-writing ids.
//
// It reads the JSON of `sweb dns records <domain> -o json` on stdin and emits one
// import block per record to stdout. Pair it with `terraform plan
// -generate-config-out` to synthesize the resource HCL:
//
//	SWEB_TOKEN=$(sweb token --profile hosting) \
//	  sweb dns records sanch.pet -o json \
//	  | tf-dns-import sanch.pet > imports.tf
//	terraform plan -generate-config-out=generated.tf
//
// The ids come from internal/importid, the same grammar the provider's
// ImportState parses — the tool and the resource can never drift.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/sanchpet/terraform-provider-sweb/internal/importid"

	"github.com/sanchpet/sweb-go-sdk/dns"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] == "" {
		fmt.Fprintln(os.Stderr, "usage: sweb dns records <domain> -o json | tf-dns-import <domain>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "tf-dns-import:", err)
		os.Exit(1)
	}
}

func run(domain string, in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	var recs []dns.Record
	if err := json.Unmarshal(raw, &recs); err != nil {
		return fmt.Errorf("parse zone json (expected `sweb dns records %s -o json`): %w", domain, err)
	}
	for _, b := range blocks(domain, recs) {
		if _, err := fmt.Fprintln(out, b); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "%d import blocks for %s\n", len(recs), domain)
	return nil
}

// blocks renders one import{} block per record, with unique HCL labels.
func blocks(domain string, recs []dns.Record) []string {
	seen := map[string]int{}
	var out []string
	for _, rec := range recs {
		host := importid.Host(rec.Type, rec.Name, rec.Domain)
		var rtype, id, label string
		if strings.EqualFold(rec.Type, "SRV") {
			rtype = "sweb_dns_srv_record"
			id = importid.SRV(domain, rec.Service, rec.Protocol, host, rec.Target, int(rec.Port))
			label = slug(rec.Service, rec.Protocol, hostLabel(host))
		} else {
			rtype = "sweb_dns_record"
			id = importid.Record(domain, rec.Type, host, rec.Value)
			label = slug(rec.Type, hostLabel(host), valueTail(rec.Value))
		}
		seen[label]++
		if n := seen[label]; n > 1 { // disambiguate a repeated label (e.g. round-robin A)
			label = fmt.Sprintf("%s_%d", label, n)
		}
		out = append(out, fmt.Sprintf("import {\n  to = %s.%s\n  id = %q\n}\n", rtype, label, id))
	}
	return out
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]+`)

// slug builds a lowercase, underscore-joined HCL identifier from its parts.
func slug(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	s := strings.Trim(nonAlnum.ReplaceAllString(strings.Join(kept, "_"), "_"), "_")
	s = strings.ToLower(s)
	if s == "" {
		return "rec"
	}
	return s
}

func hostLabel(host string) string {
	if host == "" {
		return "apex"
	}
	return host
}

// valueTail keeps a short, stable tail of the value so labels stay readable for
// long records (base64 TXT) while different values still yield different labels.
func valueTail(v string) string {
	s := nonAlnum.ReplaceAllString(v, "_")
	if len(s) > 12 {
		s = s[len(s)-12:]
	}
	return strings.Trim(s, "_")
}
