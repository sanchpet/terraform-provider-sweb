package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tftest "github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// dbaasTestProvider wraps the real provider but adds the DBaaS resource to its
// registry. In this branch provider.go does not yet register
// NewDBaaSInstanceResource (the orchestrator wires all cloud-tier resources in
// during integration), so the acceptance test serves the resource through this
// thin override — reusing the real schema/Configure via the embedded provider.
type dbaasTestProvider struct{ *swebProvider }

func (p *dbaasTestProvider) Resources(ctx context.Context) []func() resource.Resource {
	return append(p.swebProvider.Resources(ctx), NewDBaaSInstanceResource)
}

// dbaasProtoV6ProviderFactories serves the provider with the DBaaS resource wired
// in, independent of the shared factory.
var dbaasProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"sweb": providerserver.NewProtocol6WithError(func() provider.Provider {
		return &dbaasTestProvider{swebProvider: &swebProvider{version: "test"}}
	}()),
}

// TestAccDBaaSInstanceResource exercises create → read → update → import → destroy
// of a managed-database cluster against the mock (createInstance/index/
// editInstance/removeInstance on /dbaas). The update changes display_name and a
// user's password (in place via editInstance). User passwords are write-only, so
// they are ignored on import verification. Requires TF_ACC=1 and terraform on PATH.
func TestAccDBaaSInstanceResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	tftest.Test(t, tftest.TestCase{
		ProtoV6ProviderFactories: dbaasProtoV6ProviderFactories,
		Steps: []tftest.TestStep{
			{ // create
				Config: testAccDBaaSInstanceConfig(mock.URL, 100, "analytics", "app", "S3cret!"),
				Check: tftest.ComposeAggregateTestCheckFunc(
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "engine_type", "PostgreSQL"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "engine_version", "16"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "plan_id", "100"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "display_name", "analytics"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "status", "running"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "active", "true"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "ip", "203.0.113.70:5432"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "engine", "PostgreSQL"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "billing_id", "petrovpet2_dbaas_1"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "id", "petrovpet2_dbaas_1"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "endpoints.0", "rw=203.0.113.70:5432"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "user.0.name", "app"),
				),
			},
			{ // update: rename (display_name) + rotate the user password, both in place
				Config: testAccDBaaSInstanceConfig(mock.URL, 100, "reporting", "app", "N3wPass!"),
				Check: tftest.ComposeAggregateTestCheckFunc(
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "display_name", "reporting"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "user.0.password", "N3wPass!"),
					tftest.TestCheckResourceAttr("sweb_dbaas_instance.test", "billing_id", "petrovpet2_dbaas_1"),
				),
			},
			{ // import by billing_id; user passwords are write-only, not API-reported
				ResourceName:            "sweb_dbaas_instance.test",
				ImportState:             true,
				ImportStateId:           "petrovpet2_dbaas_1",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"engine_version", "user"},
			},
		},
	})
}

func testAccDBaaSInstanceConfig(endpoint string, planID int, displayName, user, password string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_dbaas_instance" "test" {
  engine_type    = "PostgreSQL"
  engine_version = "16"
  plan_id        = %[2]d
  display_name   = %[3]q

  user {
    name     = %[4]q
    password = %[5]q
  }
}
`, endpoint, planID, displayName, user, password)
}
