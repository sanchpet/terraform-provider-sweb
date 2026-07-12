package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDomainRedirectResource exercises set → update → import of a domain's
// redirect against the mock (setRedirectVh/getRedirectVh). Requires TF_ACC=1 and
// terraform on PATH.
func TestAccDomainRedirectResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // set
				Config: testAccDomainRedirectConfig(mock.URL, "example.com", "https://example.org"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_domain_redirect.test", "domain", "example.com"),
					resource.TestCheckResourceAttr("sweb_domain_redirect.test", "url", "https://example.org"),
					resource.TestCheckResourceAttr("sweb_domain_redirect.test", "id", "example.com"),
				),
			},
			{ // update the URL in place
				Config: testAccDomainRedirectConfig(mock.URL, "example.com", "https://example.net"),
				Check:  resource.TestCheckResourceAttr("sweb_domain_redirect.test", "url", "https://example.net"),
			},
			{ // import by domain
				ResourceName:      "sweb_domain_redirect.test",
				ImportState:       true,
				ImportStateId:     "example.com",
				ImportStateVerify: true,
			},
		},
	})
}

func testAccDomainRedirectConfig(endpoint, domain, url string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_domain_redirect" "test" {
  domain = %[2]q
  url    = %[3]q
}
`, endpoint, domain, url)
}
