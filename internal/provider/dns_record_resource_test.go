package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
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
