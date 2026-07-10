package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccBackupSettingsResource exercises set → read → update → import of a VPS's
// auto-backup schedule against the mock (getSettings/saveSettings). Requires
// TF_ACC=1 and terraform on PATH.
func TestAccBackupSettingsResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // set an auto schedule
				Config: testAccBackupSettingsConfig(mock.URL, "petrovpet2_vps_10", 7, 3),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_vps_backup_settings.test", "billing_id", "petrovpet2_vps_10"),
					resource.TestCheckResourceAttr("sweb_vps_backup_settings.test", "mode", "auto"),
					resource.TestCheckResourceAttr("sweb_vps_backup_settings.test", "frequency", "7"),
					resource.TestCheckResourceAttr("sweb_vps_backup_settings.test", "time", "3"),
					resource.TestCheckResourceAttr("sweb_vps_backup_settings.test", "id", "petrovpet2_vps_10"),
				),
			},
			{ // change the frequency in place
				Config: testAccBackupSettingsConfig(mock.URL, "petrovpet2_vps_10", 1, 5),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_vps_backup_settings.test", "frequency", "1"),
					resource.TestCheckResourceAttr("sweb_vps_backup_settings.test", "time", "5"),
				),
			},
			{ // import by billing_id
				ResourceName:      "sweb_vps_backup_settings.test",
				ImportState:       true,
				ImportStateId:     "petrovpet2_vps_10",
				ImportStateVerify: true,
			},
		},
	})
}

func testAccBackupSettingsConfig(endpoint, billingID string, frequency, backupTime int) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_vps_backup_settings" "test" {
  billing_id = %[2]q
  mode       = "auto"
  frequency  = %[3]d
  time       = %[4]d
}
`, endpoint, billingID, frequency, backupTime)
}
