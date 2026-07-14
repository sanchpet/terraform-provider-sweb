package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// monitoringCheckTestFactories wires an in-process provider that additionally
// registers sweb_monitoring_check. The shared provider's Resources() list is
// owned by the orchestrator (which registers NewMonitoringCheckResource on
// merge), so this branch adds the resource locally for its own acceptance test
// rather than editing provider.go.
var monitoringCheckTestFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"sweb": providerserver.NewProtocol6WithError(&monitoringCheckTestProvider{swebProvider{version: "test"}}),
}

// monitoringCheckTestProvider embeds the real provider and appends the
// monitoring-check resource to its registry.
type monitoringCheckTestProvider struct{ swebProvider }

// Resources appends the monitoring-check factory, unless the shared registry
// already carries it (once the orchestrator registers it on merge, the append
// would otherwise duplicate the type name and panic).
func (p *monitoringCheckTestProvider) Resources(ctx context.Context) []func() fwresource.Resource {
	base := p.swebProvider.Resources(ctx)
	for _, f := range base {
		var md fwresource.MetadataResponse
		f().Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: "sweb"}, &md)
		if md.TypeName == "sweb_monitoring_check" {
			return base // already registered by the orchestrator
		}
	}
	return append(base, NewMonitoringCheckResource)
}

// ensure the wrapper still satisfies provider.Provider.
var _ provider.Provider = (*monitoringCheckTestProvider)(nil)

// TestAccMonitoringCheckResource exercises create → read → update (in-place edit
// + enabled toggle) → import → destroy of a monitoring check against the mock
// (create/index/edit/activate/deactivate/remove on /monitoring/checks). Requires
// TF_ACC=1 and terraform on PATH.
func TestAccMonitoringCheckResource(t *testing.T) {
	mock := newMockSweb()
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: monitoringCheckTestFactories,
		Steps: []resource.TestStep{
			{ // create (enabled defaults to true)
				Config: testAccMonitoringCheckConfig(mock.URL, "web", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "type", "http"),
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "name", "web"),
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "target", "https://example.com"),
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "interval", "5"),
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "ssl", "true"),
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "enabled", "true"),
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "contact_ids.#", "2"),
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "id", "1"),
				),
			},
			{ // update name in place + disable via deactivate
				Config: testAccMonitoringCheckConfig(mock.URL, "web-renamed", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "name", "web-renamed"),
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "enabled", "false"),
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "id", "1"), // no replacement
				),
			},
			{ // re-enable via activate
				Config: testAccMonitoringCheckConfig(mock.URL, "web-renamed", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_monitoring_check.test", "enabled", "true"),
				),
			},
			{ // import by numeric id
				ResourceName:      "sweb_monitoring_check.test",
				ImportState:       true,
				ImportStateId:     "1",
				ImportStateVerify: true,
				// target/interval/contacts/port/ssl/keywords are inputs the index list
				// does not report, so they are not recovered on import.
				ImportStateVerifyIgnore: []string{
					"target", "interval", "contact_ids", "port", "ssl", "keywords", "keyword_mode", "type",
				},
			},
		},
	})
}

func testAccMonitoringCheckConfig(endpoint, name string, enabled bool) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_monitoring_check" "test" {
  type        = "http"
  target      = "https://example.com"
  name        = %[2]q
  interval    = 5
  contact_ids = [11, 22]
  ssl         = true
  enabled     = %[3]t
}
`, endpoint, name, enabled)
}
