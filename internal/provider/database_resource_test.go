package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDatabaseResource exercises create → read → update (password + comment in
// place) → import → destroy of a MySQL database against the mock (databaseMysqlCreate/
// databaseGetList/databaseMysqlChangePass/databaseEditComment/databaseMysqlDelete).
// Requires TF_ACC=1 and terraform on PATH.
func TestAccDatabaseResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccDatabaseConfig(mock.URL, "S3cret!", "app db"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_database.test", "name", "appdb"),
					resource.TestCheckResourceAttr("sweb_database.test", "id", "appdb"),
					resource.TestCheckResourceAttr("sweb_database.test", "full_name", "appdb"),
					resource.TestCheckResourceAttr("sweb_database.test", "comment", "app db"),
					resource.TestCheckResourceAttr("sweb_database.test", "version", "8.0"),
					resource.TestCheckResourceAttr("sweb_database.test", "charset", "utf8mb4"),
					resource.TestCheckResourceAttr("sweb_database.test", "login", "appdb"),
				),
			},
			{ // update password + comment in place (no replacement)
				Config: testAccDatabaseConfig(mock.URL, "N3wPass!", "renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_database.test", "password", "N3wPass!"),
					resource.TestCheckResourceAttr("sweb_database.test", "comment", "renamed"),
				),
			},
			{ // import by name; password is write-only, not API-reported
				ResourceName:            "sweb_database.test",
				ImportState:             true,
				ImportStateId:           "appdb",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
		},
	})
}

func testAccDatabaseConfig(endpoint, password, comment string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_database" "test" {
  name     = "appdb"
  password = %[2]q
  comment  = %[3]q
}
`, endpoint, password, comment)
}
