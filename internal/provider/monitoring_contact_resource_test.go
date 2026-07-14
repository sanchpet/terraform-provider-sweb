package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMonitoringContactResource exercises create → read → update (name in
// place) → import → destroy of an email monitoring contact against the mock
// (addEmail/index/editEmail/deleteContact on /monitoring/contacts). Create returns
// the contact id directly, which is the resource identity. Requires TF_ACC=1 and
// terraform on PATH.
func TestAccMonitoringContactResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccMonitoringContactConfig(mock.URL, "ops@example.com", "Ops team"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_monitoring_contact.test", "type", "email"),
					resource.TestCheckResourceAttr("sweb_monitoring_contact.test", "value", "ops@example.com"),
					resource.TestCheckResourceAttr("sweb_monitoring_contact.test", "name", "Ops team"),
					resource.TestCheckResourceAttr("sweb_monitoring_contact.test", "verified", "false"),
					resource.TestCheckResourceAttr("sweb_monitoring_contact.test", "id", "1"),
				),
			},
			{ // update name in place (no replacement)
				Config: testAccMonitoringContactConfig(mock.URL, "ops@example.com", "On-call"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_monitoring_contact.test", "name", "On-call"),
					resource.TestCheckResourceAttr("sweb_monitoring_contact.test", "id", "1"),
				),
			},
			{ // import by numeric id
				ResourceName:      "sweb_monitoring_contact.test",
				ImportState:       true,
				ImportStateId:     "1",
				ImportStateVerify: true,
			},
		},
	})
}

func testAccMonitoringContactConfig(endpoint, value, name string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_monitoring_contact" "test" {
  type  = "email"
  value = %[2]q
  name  = %[3]q
}
`, endpoint, value, name)
}
