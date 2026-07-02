package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccPlanDataSource resolves a configurator spec to a plan id against the mock
// SpaceWeb backend (getConstructorPlanId → 379). This is the id an imported
// plan-mode node keeps, so `plan = data.sweb_plan.x.id` produces a clean plan.
//
// Requires TF_ACC=1 and a terraform binary on PATH.
func TestAccPlanDataSource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPlanDataSourceConfig(mock.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.sweb_plan.test", "id", "379"),
					resource.TestCheckResourceAttr("data.sweb_plan.test", "cpu", "2"),
					resource.TestCheckResourceAttr("data.sweb_plan.test", "ram", "6"),
					resource.TestCheckResourceAttr("data.sweb_plan.test", "disk", "15"),
				),
			},
		},
	})
}

func testAccPlanDataSourceConfig(endpoint string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

data "sweb_plan" "test" {
  cpu  = 2
  ram  = 6
  disk = 15
}
`, endpoint)
}
