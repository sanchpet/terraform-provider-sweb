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
				Config: testAccDatabaseConfig(mock.URL, "S3cret!", 1, "app db"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_database.test", "name", "appdb"),
					resource.TestCheckResourceAttr("sweb_database.test", "id", "appdb"),
					resource.TestCheckResourceAttr("sweb_database.test", "full_name", "appdb"),
					resource.TestCheckResourceAttr("sweb_database.test", "comment", "app db"),
					resource.TestCheckResourceAttr("sweb_database.test", "version", "8.0"),
					resource.TestCheckResourceAttr("sweb_database.test", "charset", "utf8mb4"),
					resource.TestCheckResourceAttr("sweb_database.test", "login", "appdb"),
					resource.TestCheckNoResourceAttr("sweb_database.test", "password"),
				),
			},
			{ // rotate password (bump version) + edit comment in place (no replacement)
				Config: testAccDatabaseConfig(mock.URL, "N3wPass!", 2, "renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_database.test", "password_wo_version", "2"),
					resource.TestCheckResourceAttr("sweb_database.test", "comment", "renamed"),
					resource.TestCheckNoResourceAttr("sweb_database.test", "password"),
				),
			},
			{ // import by name; password is write-only, so it never round-trips
				ResourceName:            "sweb_database.test",
				ImportState:             true,
				ImportStateId:           "appdb",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password", "password_wo_version"},
			},
		},
	})
}

func testAccDatabaseConfig(endpoint, password string, pwVersion int, comment string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_database" "test" {
  name                = "appdb"
  password            = %[2]q
  password_wo_version = %[3]d
  comment             = %[4]q
}
`, endpoint, password, pwVersion, comment)
}
