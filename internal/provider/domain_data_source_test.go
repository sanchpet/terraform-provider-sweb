package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDomainDataSource reads a domain via the mock (getDomainInfo) and checks
// the exposed attributes. Requires TF_ACC=1 and terraform on PATH.
func TestAccDomainDataSource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccDomainDataSourceConfig(mock.URL, "example.com"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.sweb_domain.test", "domain", "example.com"),
					resource.TestCheckResourceAttr("data.sweb_domain.test", "registrar", "TEST-REGISTRAR"),
					resource.TestCheckResourceAttr("data.sweb_domain.test", "is_our", "true"),
					resource.TestCheckResourceAttr("data.sweb_domain.test", "reg_price", "189"),
					resource.TestCheckResourceAttr("data.sweb_domain.test", "transfer_price", "-1"),
					resource.TestCheckResourceAttr("data.sweb_domain.test", "docroot", "/home/e/example"),
					resource.TestCheckResourceAttr("data.sweb_domain.test", "id", "example.com"),
				),
			},
		},
	})
}

func testAccDomainDataSourceConfig(endpoint, domain string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

data "sweb_domain" "test" {
  domain = %[2]q
}
`, endpoint, domain)
}
