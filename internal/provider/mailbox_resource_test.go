package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMailboxResource exercises create → read → update → import → destroy of a
// mailbox against the mock (createMbox/getMailboxesList/updateAntispamState/
// changeMailboxSpf/updateComment/changeMailboxPassword/dropMbox). Requires TF_ACC=1
// and terraform on PATH.
func TestAccMailboxResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create with non-default antispam/spf/comment
				Config: testAccMailboxConfig(mock.URL, "S3cret!", 1, "medium", true, "primary"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_mailbox.test", "domain", "example.com"),
					resource.TestCheckResourceAttr("sweb_mailbox.test", "name", "info"),
					resource.TestCheckResourceAttr("sweb_mailbox.test", "id", "example.com/info"),
					resource.TestCheckResourceAttr("sweb_mailbox.test", "antispam", "medium"),
					resource.TestCheckResourceAttr("sweb_mailbox.test", "spf", "true"),
					resource.TestCheckResourceAttr("sweb_mailbox.test", "comment", "primary"),
					resource.TestCheckResourceAttr("sweb_mailbox.test", "quota", "1024"),
					// password is write-only — it must never be persisted to state.
					resource.TestCheckNoResourceAttr("sweb_mailbox.test", "password"),
				),
			},
			{ // update every mutable field in place; rotate the password by bumping its version
				Config: testAccMailboxConfig(mock.URL, "N3wPass!", 2, "off", false, "renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_mailbox.test", "antispam", "off"),
					resource.TestCheckResourceAttr("sweb_mailbox.test", "spf", "false"),
					resource.TestCheckResourceAttr("sweb_mailbox.test", "comment", "renamed"),
					resource.TestCheckResourceAttr("sweb_mailbox.test", "password_wo_version", "2"),
					resource.TestCheckNoResourceAttr("sweb_mailbox.test", "password"),
				),
			},
			{ // import by <domain>/<name>; password is write-only, so it never round-trips
				ResourceName:            "sweb_mailbox.test",
				ImportState:             true,
				ImportStateId:           "example.com/info",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password", "password_wo_version"},
			},
		},
	})
}

func testAccMailboxConfig(endpoint, password string, pwVersion int, antispam string, spf bool, comment string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_mailbox" "test" {
  domain              = "example.com"
  name                = "info"
  password            = %[2]q
  password_wo_version = %[3]d
  antispam            = %[4]q
  spf                 = %[5]t
  comment             = %[6]q
}
`, endpoint, password, pwVersion, antispam, spf, comment)
}
