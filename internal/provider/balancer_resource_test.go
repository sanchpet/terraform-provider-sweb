package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// TestAccBalancerResource exercises the full Terraform lifecycle of a
// sweb_balancer against the in-memory SpaceWeb mock: create via the List-diff
// correlation, an in-place update that changes type and swaps a server/rule,
// then import (by billing_id) and destroy.
//
// Requires TF_ACC=1 and a terraform binary on PATH.
func TestAccBalancerResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create + implicit post-apply clean plan
				Config: testAccBalancerConfig(mock.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("sweb_balancer.test", "billing_id"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "type", "roundrobin"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "alias", "tf-acc-lb"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "ip_balancer", "203.0.113.60"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "active", "true"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "server.#", "1"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "server.0.ip", "203.0.113.10"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "rule.#", "1"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "rule.0.port_balancer", "80"),
				),
			},
			{ // update: change type + swap the server and rule — must be in-place, not replacement
				Config: testAccBalancerConfigUpdated(mock.URL),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("sweb_balancer.test", plancheck.ResourceActionUpdate),
						// The stable computed fields stay known across an in-place edit.
						plancheck.ExpectKnownValue("sweb_balancer.test", tfjsonpath.New("ip_balancer"), knownvalue.StringExact("203.0.113.60")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_balancer.test", "type", "leastconn"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "alias", "tf-acc-lb-2"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "server.0.ip", "203.0.113.20"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "rule.0.port_balancer", "443"),
					resource.TestCheckResourceAttr("sweb_balancer.test", "rule.0.protocol_balancer", "https"),
				),
			},
			{ // import: reconstructs full state from the live list by billing_id
				ResourceName:            "sweb_balancer.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}

func testAccBalancerConfig(endpoint string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_balancer" "test" {
  datacenter = 1
  type       = "roundrobin"
  plan_id    = 4298
  alias      = "tf-acc-lb"

  server {
    ip     = "203.0.113.10"
    weight = 1
  }

  rule {
    protocol_balancer = "http"
    port_balancer     = "80"
    protocol_server   = "http"
    port_server       = "80"
  }
}
`, endpoint)
}

// testAccBalancerConfigUpdated changes the in-place fields: type, alias, the
// back-end server and the forwarding rule.
func testAccBalancerConfigUpdated(endpoint string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_balancer" "test" {
  datacenter = 1
  type       = "leastconn"
  plan_id    = 4298
  alias      = "tf-acc-lb-2"

  server {
    ip     = "203.0.113.20"
    weight = 2
  }

  rule {
    protocol_balancer = "https"
    port_balancer     = "443"
    protocol_server   = "https"
    port_server       = "443"
  }
}
`, endpoint)
}
