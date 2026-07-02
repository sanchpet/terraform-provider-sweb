package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccLocalNetworkResource exercises attach → read → import → detach of a VPS
// to the private (local) network against the mock (addLocal/removeLocal + the
// /vps/ip index reporting local_ip). Requires TF_ACC=1 and terraform on PATH.
func TestAccLocalNetworkResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // attach: addLocal → poll index → local IP assigned
				Config: testAccLocalNetworkConfig(mock.URL, "petrovpet2_vps_10"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_vps_local_network.test", "billing_id", "petrovpet2_vps_10"),
					resource.TestCheckResourceAttr("sweb_vps_local_network.test", "id", "petrovpet2_vps_10"),
					resource.TestCheckResourceAttr("sweb_vps_local_network.test", "local_ip", "10.0.0.24"),
					resource.TestCheckResourceAttr("sweb_vps_local_network.test", "mask", "10.0.0.0/27"),
					resource.TestCheckResourceAttr("sweb_vps_local_network.test", "mac", "00:16:3e:aa:bb:cc"),
				),
			},
			{ // import by billing_id
				ResourceName:            "sweb_vps_local_network.test",
				ImportState:             true,
				ImportStateId:           "petrovpet2_vps_10",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}

func testAccLocalNetworkConfig(endpoint, billingID string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_vps_local_network" "test" {
  billing_id = %[2]q
}
`, endpoint, billingID)
}
