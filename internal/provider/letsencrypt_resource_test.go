package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccLetsEncryptResource exercises create → read → update (autoprolong in
// place) → import → destroy of a Let's Encrypt certificate against the mock
// (installLetsEncrypt/index/editAutoprolong/removeCertificate). Requires TF_ACC=1
// and terraform on PATH.
func TestAccLetsEncryptResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create with auto-prolongation on
				Config: testAccLetsEncryptConfig(mock.URL, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_letsencrypt.test", "domain", "example.com"),
					resource.TestCheckResourceAttr("sweb_letsencrypt.test", "id", "example.com"),
					resource.TestCheckResourceAttr("sweb_letsencrypt.test", "wildcard", "false"),
					resource.TestCheckResourceAttr("sweb_letsencrypt.test", "autoprolong", "true"),
					resource.TestCheckResourceAttr("sweb_letsencrypt.test", "status", "active"),
					resource.TestCheckResourceAttr("sweb_letsencrypt.test", "valid_to", "2027-01-01"),
					resource.TestCheckResourceAttrSet("sweb_letsencrypt.test", "certificate_id"),
				),
			},
			{ // toggle auto-prolongation off in place (no replacement)
				Config: testAccLetsEncryptConfig(mock.URL, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_letsencrypt.test", "autoprolong", "false"),
				),
			},
			{ // import by domain; issuance inputs aren't recoverable from the list
				ResourceName:            "sweb_letsencrypt.test",
				ImportState:             true,
				ImportStateId:           "example.com",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"wildcard"},
			},
		},
	})
}

func testAccLetsEncryptConfig(endpoint string, autoprolong bool) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_letsencrypt" "test" {
  domain      = "example.com"
  autoprolong = %[2]t
}
`, endpoint, autoprolong)
}
