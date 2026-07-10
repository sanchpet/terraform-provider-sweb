package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccPTRRecordResource exercises set → read → update → import of an IP's PTR
// record against the mock (editPtr/getPtr). Requires TF_ACC=1 and terraform on
// PATH.
func TestAccPTRRecordResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // set
				Config: testAccPTRRecordConfig(mock.URL, "203.0.113.7", "host.example.com"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_ptr_record.test", "ip", "203.0.113.7"),
					resource.TestCheckResourceAttr("sweb_ptr_record.test", "ptr", "host.example.com"),
					resource.TestCheckResourceAttr("sweb_ptr_record.test", "id", "203.0.113.7"),
				),
			},
			{ // update the PTR in place
				Config: testAccPTRRecordConfig(mock.URL, "203.0.113.7", "mail.example.com"),
				Check:  resource.TestCheckResourceAttr("sweb_ptr_record.test", "ptr", "mail.example.com"),
			},
			{ // import by ip
				ResourceName:      "sweb_ptr_record.test",
				ImportState:       true,
				ImportStateId:     "203.0.113.7",
				ImportStateVerify: true,
			},
		},
	})
}

func testAccPTRRecordConfig(endpoint, ip, ptr string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_ptr_record" "test" {
  ip  = %[2]q
  ptr = %[3]q
}
`, endpoint, ip, ptr)
}
