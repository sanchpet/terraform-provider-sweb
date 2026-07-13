package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestAccCronTaskResource exercises create → read → replace → import → destroy of a
// cron task against the mock (addTask/getTasks/removeTask). Every attribute forces
// replacement, so changing the schedule replaces the entry. Requires TF_ACC=1 and
// terraform on PATH.
func TestAccCronTaskResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccCronTaskConfig(mock.URL, 30, "backup.sh"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_cron_task.test", "minute", "30"),
					resource.TestCheckResourceAttr("sweb_cron_task.test", "hour", "12"),
					resource.TestCheckResourceAttr("sweb_cron_task.test", "command", "backup.sh"),
					resource.TestCheckResourceAttr("sweb_cron_task.test", "id", "30 12 1 12 7 backup.sh"),
				),
			},
			{ // change the minute → forces replacement (new id)
				Config: testAccCronTaskConfig(mock.URL, 45, "backup.sh"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("sweb_cron_task.test", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_cron_task.test", "minute", "45"),
					resource.TestCheckResourceAttr("sweb_cron_task.test", "id", "45 12 1 12 7 backup.sh"),
				),
			},
			{ // import by the raw crontab line
				ResourceName:      "sweb_cron_task.test",
				ImportState:       true,
				ImportStateId:     "45 12 1 12 7 backup.sh",
				ImportStateVerify: true,
			},
		},
	})
}

func testAccCronTaskConfig(endpoint string, minute int, command string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_cron_task" "test" {
  minute  = %[2]d
  hour    = 12
  day     = 1
  month   = 12
  weekday = 7
  command = %[3]q
}
`, endpoint, minute, command)
}
