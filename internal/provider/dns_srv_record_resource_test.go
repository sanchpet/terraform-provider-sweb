package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDNSSRVRecordResource exercises create → read → import → destroy of an
// SRV record against the mock (editSrv + info). Requires TF_ACC=1 and terraform
// on PATH.
func TestAccDNSSRVRecordResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccDNSSRVRecordConfig(mock.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_dns_srv_record.sip", "service", "sip"),
					resource.TestCheckResourceAttr("sweb_dns_srv_record.sip", "protocol", "tcp"),
					resource.TestCheckResourceAttr("sweb_dns_srv_record.sip", "port", "5060"),
					resource.TestCheckResourceAttr("sweb_dns_srv_record.sip", "priority", "10"),
					resource.TestCheckResourceAttr("sweb_dns_srv_record.sip", "weight", "20"),
					resource.TestCheckResourceAttr("sweb_dns_srv_record.sip", "id", "example.com/sip/tcp//sip.example.com./5060"),
				),
			},
			{ // import by <domain>/<service>/<protocol>/<name>/<target>/<port>
				ResourceName:      "sweb_dns_srv_record.sip",
				ImportState:       true,
				ImportStateId:     "example.com/sip/tcp//sip.example.com./5060",
				ImportStateVerify: true,
			},
		},
	})
}

func testAccDNSSRVRecordConfig(endpoint string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_dns_srv_record" "sip" {
  domain   = "example.com"
  service  = "sip"
  protocol = "tcp"
  target   = "sip.example.com."
  port     = 5060
  priority = 10
  weight   = 20
  ttl      = 3600
}
`, endpoint)
}
