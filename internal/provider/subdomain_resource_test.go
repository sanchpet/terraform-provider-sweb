package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccSubdomainResource exercises create → read → import of a subdomain
// against the mock (createSubdomain/getSubdomains/removeSubdomain). Requires
// TF_ACC=1 and terraform on PATH.
func TestAccSubdomainResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccSubdomainConfig(mock.URL, "example.com", "shop", "/shop"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_subdomain.test", "domain", "example.com"),
					resource.TestCheckResourceAttr("sweb_subdomain.test", "machine", "shop"),
					resource.TestCheckResourceAttr("sweb_subdomain.test", "id", "example.com/shop"),
				),
			},
			{ // import by <domain>/<machine>; dir is create-only and not refreshed
				ResourceName:            "sweb_subdomain.test",
				ImportState:             true,
				ImportStateId:           "example.com/shop",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"dir"},
			},
		},
	})
}

func testAccSubdomainConfig(endpoint, domain, machine, dir string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_subdomain" "test" {
  domain  = %[2]q
  machine = %[3]q
  dir     = %[4]q
}
`, endpoint, domain, machine, dir)
}
