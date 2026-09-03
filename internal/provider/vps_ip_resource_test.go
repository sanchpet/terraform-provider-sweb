package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	swebip "github.com/sanchpet/sweb-go-sdk/ip"
)

const testAccVPSIPBillingID = "petrovpet2_vps_10"

// TestAccVPSIPResource covers order → correlate → read → import against the mock
// (/vps/ip add/index). The VPS starts out holding an unrelated address, so the
// correlation has to pick the one this apply ordered rather than "the first IP on
// the VPS" — the whole point of the list-diff.
func TestAccVPSIPResource(t *testing.T) {
	mock, state := newMockSwebDelayedDNS(0)
	defer mock.Close()
	state.publicIPs = map[string][]swebip.Address{
		testAccVPSIPBillingID: {{IP: "203.0.113.10", Gateway: "203.0.113.1", Netmask: "203.0.113.0/24"}},
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // order one IP, then find it by diffing the VPS's IP list
				Config: testAccVPSIPConfig(mock.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sweb_vps_ip.test", "billing_id", testAccVPSIPBillingID),
					resource.TestCheckResourceAttr("sweb_vps_ip.test", "ip", "203.0.113.101"),
					resource.TestCheckResourceAttr("sweb_vps_ip.test", "id", testAccVPSIPBillingID+":203.0.113.101"),
					resource.TestCheckResourceAttr("sweb_vps_ip.test", "gateway", "203.0.113.1"),
					resource.TestCheckResourceAttr("sweb_vps_ip.test", "netmask", "203.0.113.0/24"),
				),
			},
			{ // adopt an address that already exists, by <billing_id>:<ip>
				ResourceName:            "sweb_vps_ip.test",
				ImportState:             true,
				ImportStateId:           testAccVPSIPBillingID + ":203.0.113.101",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{ // the VPS alone is not an address
				ResourceName:  "sweb_vps_ip.test",
				ImportState:   true,
				ImportStateId: testAccVPSIPBillingID,
				ExpectError:   regexp.MustCompile(`Invalid import id`),
			},
		},
	})
}

// TestAccVPSIPBusyOrder pins the retry: SpaceWeb takes one operation per VPS at a
// time and refuses the rest with -32500, which must not fail the apply.
func TestAccVPSIPBusyOrder(t *testing.T) {
	mock, state := newMockSwebDelayedDNS(0)
	defer mock.Close()
	state.ipBusyOrders = 1

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccVPSIPConfig(mock.URL),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("sweb_vps_ip.test", "ip", "203.0.113.101"),
				func(*terraform.State) error {
					state.mu.Lock()
					defer state.mu.Unlock()
					if state.ipBusyOrders != 0 {
						return fmt.Errorf("the busy refusal was never served, so the retry was not exercised")
					}
					if got := len(state.publicIPs[testAccVPSIPBillingID]); got != 1 {
						return fmt.Errorf("VPS holds %d IPs after one apply, want 1 — the retry double-ordered", got)
					}
					return nil
				},
			),
		}},
	})
}

// TestAccVPSIP24hLock pins the refusal an IP younger than 24h draws on release:
// the destroy fails with an error that says what to do, and the resource stays in
// state (the test's own cleanup destroy then succeeds, the lock having lapsed).
func TestAccVPSIP24hLock(t *testing.T) {
	mock, state := newMockSwebDelayedDNS(0)
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: testAccVPSIPConfig(mock.URL)},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.ipRemoveLocked = 1
				},
				Config:      testAccVPSIPConfig(mock.URL),
				Destroy:     true,
				ExpectError: tfDetail("re-run", "destroy", "after", "the", "lock", "expires"),
			},
		},
	})
}

// TestAccVPSIPDrift covers the address released outside Terraform (the CLI, the
// panel): the refresh must drop the resource instead of reporting an IP that is
// no longer on the VPS.
func TestAccVPSIPDrift(t *testing.T) {
	mock, state := newMockSwebDelayedDNS(0)
	defer mock.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPSIPConfig(mock.URL),
				Check:  resource.TestCheckResourceAttr("sweb_vps_ip.test", "ip", "203.0.113.101"),
			},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.publicIPs[testAccVPSIPBillingID] = nil
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true, // the IP is gone; the next apply orders another
				Check:              testCheckResourceGone("sweb_vps_ip.test"),
			},
		},
	})
}

func testAccVPSIPConfig(endpoint string) string {
	return fmt.Sprintf(`
provider "sweb" {
  endpoint = %[1]q
  token    = "test-token"
}

resource "sweb_vps_ip" "test" {
  billing_id = %[2]q
}
`, endpoint, testAccVPSIPBillingID)
}

// testCheckResourceGone asserts a resource is absent from state — what a Read
// that hit RemoveResource leaves behind.
func testCheckResourceGone(name string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if _, ok := s.RootModule().Resources[name]; ok {
			return fmt.Errorf("%s is still in state, want dropped by the refresh", name)
		}
		return nil
	}
}

// tfDetail matches a phrase inside a rendered Terraform diagnostic, tolerating
// the line wrapping and "│" gutter the CLI adds to the detail block.
func tfDetail(words ...string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)` + strings.Join(words, `[\s│]+`))
}
