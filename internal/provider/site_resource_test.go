package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccSiteResource exercises create → read → update (alias in place) → import →
// destroy of a website against the mock (add/index/edit/del on /sites). Requires
// TF_ACC=1 and terraform on PATH.
func TestAccSiteResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccSiteConfig(mock.URL, "my-site"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_site.test", "alias", "my-site"),
					resource.TestCheckResourceAttr("sweb_site.test", "doc_root", "example"),
					resource.TestCheckResourceAttr("sweb_site.test", "domain", "example.com"),
					resource.TestCheckResourceAttr("sweb_site.test", "id", "example"),
					resource.TestCheckResourceAttr("sweb_site.test", "doc_root_full", "/home/example"),
					resource.TestCheckResourceAttrSet("sweb_site.test", "site_id"),
				),
			},
			{ // rename in place (alias not ForceNew)
				Config: testAccSiteConfig(mock.URL, "renamed-site"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_site.test", "alias", "renamed-site"),
					resource.TestCheckResourceAttr("sweb_site.test", "doc_root", "example"),
				),
			},
			{ // import by doc_root; binding inputs aren't per-site API-reported
				ResourceName:            "sweb_site.test",
				ImportState:             true,
				ImportStateId:           "example",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"domain", "machine", "enable_redis_session"},
			},
		},
	})
}

func testAccSiteConfig(endpoint, alias string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_site" "test" {
  alias    = %[2]q
  doc_root = "example"
  domain   = "example.com"
}
`, endpoint, alias)
}
