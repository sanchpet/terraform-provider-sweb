package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
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
			{ // rename: alias change must be an in-place update, never a replacement
				Config: testAccVPSConfigRenamed(mock.URL),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("sweb_vps.test", plancheck.ResourceActionUpdate),
						// A rename must not churn the stable computed fields: they stay known
						// (UseStateForUnknown), not "known after apply".
						plancheck.ExpectKnownValue("sweb_vps.test", tfjsonpath.New("uid"), knownvalue.StringExact("uid-1")),
						plancheck.ExpectKnownValue("sweb_vps.test", tfjsonpath.New("ip"), knownvalue.StringExact("203.0.113.50")),
						plancheck.ExpectKnownValue("sweb_vps.test", tfjsonpath.New("running"), knownvalue.Bool(true)),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_vps.test", "alias", "tf-acc-renamed"),
					resource.TestCheckResourceAttr("sweb_vps.test", "name", "tf-acc-renamed"),
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

// testAccVPSConfigRenamed is testAccVPSConfig with only the alias changed — the
// input that the provider updates in place.
func testAccVPSConfigRenamed(endpoint string) string {
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
  alias        = "tf-acc-renamed"
}
`, endpoint)
}
