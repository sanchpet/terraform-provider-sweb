package provider

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccDNSRecordResource exercises create → read → import → destroy across the
// A, MX, and TXT record types against the mock (editMain/editMx/editTxt + info).
// Requires TF_ACC=1 and terraform on PATH.
func TestAccDNSRecordResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create A + MX + TXT in one zone
				Config: testAccDNSRecordConfig(mock.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_dns_record.a", "id", "example.com/A/www/203.0.113.7"),
					resource.TestCheckResourceAttr("sweb_dns_record.a", "name", "www"),
					resource.TestCheckResourceAttr("sweb_dns_record.mx", "type", "MX"),
					resource.TestCheckResourceAttr("sweb_dns_record.mx", "priority", "10"),
					resource.TestCheckResourceAttr("sweb_dns_record.txt", "type", "TXT"),
					resource.TestCheckResourceAttr("sweb_dns_record.txt", "value", "v=spf1 ~all"),
				),
			},
			{ // import the A record by <domain>/<type>/<name>/<value>
				ResourceName:      "sweb_dns_record.a",
				ImportState:       true,
				ImportStateId:     "example.com/A/www/203.0.113.7",
				ImportStateVerify: true,
			},
		},
	})
}

func testAccDNSRecordConfig(endpoint string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_dns_record" "a" {
  domain = "example.com"
  type   = "A"
  name   = "www"
  value  = "203.0.113.7"
}

resource "sweb_dns_record" "mx" {
  domain   = "example.com"
  type     = "MX"
  value    = "mx1.example.com."
  priority = 10
}

resource "sweb_dns_record" "txt" {
  domain = "example.com"
  type   = "TXT"
  name   = "@"
  value  = "v=spf1 ~all"
}
`, endpoint)
}

// TestAccDNSRecordConcurrentDeletes is the regression test for the batch-delete
// data loss: SpaceWeb addresses a record by its position in the zone, so several
// deletes in one apply each derived an index from the same pre-mutation zone and
// every delete after the first removed whichever record had shifted into that
// position — destroying records outside the plan.
//
// The zone read is delayed so the deletes overlap the way they do over the real
// network. The assertion is on the mock's zone, not on Terraform state: state
// records the deletes as successful either way, which is why the incident was
// invisible to `terraform plan`.
func TestAccDNSRecordConcurrentDeletes(t *testing.T) {
	srv, mock := newMockSwebDelayedDNS(100 * time.Millisecond)
	defer srv.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // six A records in one zone
				Config: testAccDNSRecordZoneConfig(srv.URL, "a", "b", "c", "d", "e", "f"),
			},
			{ // drop four of them in one apply; the survivors must be exactly the rest
				Config: testAccDNSRecordZoneConfig(srv.URL, "b", "d"),
				Check:  func(*terraform.State) error { return mock.checkZone("example.com", "b", "d") },
			},
		},
	})
}

// testAccDNSRecordZoneConfig declares one A record per host label, each with a
// value derived from the label so a record keeps its identity across steps.
func testAccDNSRecordZoneConfig(endpoint string, hosts ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `
provider "sweb" {
  endpoint = %q
  token    = "test-token"
}
`, endpoint)
	for _, h := range hosts {
		fmt.Fprintf(&b, `
resource "sweb_dns_record" %[1]q {
  domain = "example.com"
  type   = "A"
  name   = %[1]q
  value  = "203.0.113.%[2]d"
}
`, h, 10+int(h[0]-'a'))
	}
	return b.String()
}

// checkZone asserts the domain holds exactly the named hosts and that no delete
// was addressed by an index the zone had already invalidated.
func (m *mockSweb) checkZone(domain string, want ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	got := make([]string, 0, len(m.dnsRecords[domain]))
	for _, rec := range m.dnsRecords[domain] {
		got = append(got, rec.Name)
	}
	slices.Sort(got)
	slices.Sort(want)
	var problems []string
	if !slices.Equal(got, want) {
		problems = append(problems, fmt.Sprintf("zone holds %v, want exactly %v", got, want))
	}
	if n := len(m.dnsStaleDeletes); n > 0 {
		problems = append(problems, fmt.Sprintf("%d delete(s) addressed the zone by a stale index: %v", n, m.dnsStaleDeletes))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}
