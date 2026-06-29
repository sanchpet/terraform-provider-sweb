package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccVPSResource exercises the full Terraform lifecycle against the mock
// SpaceWeb backend: create via the configurator, an implicit clean-plan check,
// then import (which reconstructs the node in plan-mode) and destroy.
//
// Requires TF_ACC=1 and a terraform binary on PATH.
func TestAccVPSResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create + implicit post-apply clean plan
				Config: testAccVPSConfig(mock.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("sweb_vps.test", "billing_id"),
					resource.TestCheckResourceAttrSet("sweb_vps.test", "uid"),
					resource.TestCheckResourceAttr("sweb_vps.test", "name", "tf-acc"),
					resource.TestCheckResourceAttr("sweb_vps.test", "ip", "203.0.113.50"),
					resource.TestCheckResourceAttr("sweb_vps.test", "running", "true"),
				),
			},
			{ // import: reconstructs in plan-mode, so configurator inputs differ by design
				ResourceName:      "sweb_vps.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"cpu", "ram", "disk", "category", "plan", "timeouts",
				},
			},
		},
	})
}

func testAccVPSConfig(endpoint string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_vps" "test" {
  cpu          = 2
  ram          = 6
  disk         = 15
  category     = 1
  distributive = 164
  datacenter   = 1
  alias        = "tf-acc"
}
`, endpoint)
}
