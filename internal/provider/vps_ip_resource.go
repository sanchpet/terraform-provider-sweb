package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/apierr"
	swebip "github.com/sanchpet/sweb-go-sdk/ip"
)

// vpsIPLocks serializes IP orders per VPS — see the comment in Create.
var vpsIPLocks keyedMutex

const (
	// ipPollInterval is how often Create re-reads the VPS IP list while an
	// ordered address settles. SpaceWeb assigns it asynchronously, minutes late
	// at times, which is why the create timeout is generous.
	ipPollInterval = 10 * time.Second
	// ipBusyBackoffFirst/Cap bound the retry of an order the API refused because
	// another operation was already running on the VPS.
	ipBusyBackoffFirst = 2 * time.Second
	ipBusyBackoffCap   = 30 * time.Second
)

var (
	_ resource.Resource                = (*vpsIPResource)(nil)
	_ resource.ResourceWithImportState = (*vpsIPResource)(nil)
)

// NewVPSIPResource is the resource factory registered with the provider.
func NewVPSIPResource() resource.Resource { return &vpsIPResource{} }

// vpsIPResource is one additional public IP on a VPS (add/remove on /vps/ip).
// It exists so a burnt address can be rotated declaratively — replacing this
// resource releases the old IP and orders a new one — instead of the CLI plus a
// hand edit. sweb_vps.ip_count still covers the IPs ordered at create time;
// mixing the two on one VPS makes ip_count drift, so pick one per node.
type vpsIPResource struct{ client *sweb.Client }

type vpsIPModel struct {
	BillingID types.String   `tfsdk:"billing_id"`
	ID        types.String   `tfsdk:"id"`
	IP        types.String   `tfsdk:"ip"`
	Gateway   types.String   `tfsdk:"gateway"`
	Netmask   types.String   `tfsdk:"netmask"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func (r *vpsIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vps_ip"
}

func (r *vpsIPResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keepStr := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		Description: "One additional public IP on a VPS (add/remove on /vps/ip). SpaceWeb picks the " +
			"address, so it is computed: replacing the resource releases the old IP and orders a new " +
			"one, which is how a burnt address is rotated. Ordering bills, and an IP cannot be " +
			"released within 24h of being ordered. The guest OS still needs the address configured on " +
			"its interface.",
		Attributes: map[string]schema.Attribute{
			"billing_id": schema.StringAttribute{
				Required:      true,
				Description:   "VPS service id (login_vps_N) to order the IP for. Changing it moves the IP to another VPS by replacement (release + order), not by the API's move.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Terraform identifier — `<billing_id>:<ip>`, the import key.",
				PlanModifiers: keepStr,
			},
			"ip": schema.StringAttribute{
				Computed:      true,
				Description:   "The assigned public IP. Chosen by SpaceWeb, not requestable.",
				PlanModifiers: keepStr,
			},
			"gateway": schema.StringAttribute{Computed: true, Description: "Gateway for the address.", PlanModifiers: keepStr},
			"netmask": schema.StringAttribute{Computed: true, Description: "Netmask for the address, in CIDR form.", PlanModifiers: keepStr},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *vpsIPResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*sweb.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *sweb.Client, got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *vpsIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpsIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	billingID := plan.BillingID.ValueString()

	createTimeout, diags := plan.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	// One order at a time per VPS. "add" answers nothing usable, so the new
	// address is correlated by diffing the VPS's IP list before and after — only
	// unambiguous while a single order is in flight (same idiom as createMu in
	// vps_resource.go). The API refuses concurrent operations on a VPS anyway.
	defer vpsIPLocks.lock(billingID)()

	before, err := r.addresses(ctx, billingID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list the VPS IPs before ordering", err.Error())
		return
	}
	if err := r.orderIP(ctx, billingID, before); err != nil {
		resp.Diagnostics.AddError("Failed to order an additional IP", err.Error())
		return
	}
	addr, err := r.waitForNewAddress(ctx, billingID, before)
	if err != nil {
		resp.Diagnostics.AddError("The ordered IP did not appear",
			fmt.Sprintf("%s\n\nThe order is billed once placed, so it may still land. Check "+
				"`sweb vps ip list %s` and import the address instead of ordering another one:\n"+
				"  terraform import <address> %s:<ip>", err, billingID, billingID))
		return
	}

	plan.applyAddress(billingID, addr)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpsIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpsIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	billingID := state.BillingID.ValueString()

	addrs, err := r.addresses(ctx, billingID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the VPS IPs", err.Error())
		return
	}
	addr, ok := addrs[state.IP.ValueString()]
	if !ok {
		resp.State.RemoveResource(ctx) // released out of band → drop from state
		return
	}
	state.applyAddress(billingID, addr)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update has no mutable attributes (billing_id forces replacement, the rest are
// computed) — it only runs for timeouts-block changes. Carry the computed values
// forward from state so they don't go unknown.
func (r *vpsIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vpsIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID, plan.IP, plan.Gateway, plan.Netmask = state.ID, state.IP, state.Gateway, state.Netmask
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpsIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpsIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	billingID, address := state.BillingID.ValueString(), state.IP.ValueString()

	// Releasing is an operation on the VPS too, so it queues behind an order.
	defer vpsIPLocks.lock(billingID)()

	err := r.client.IP.Remove(ctx, billingID, address)
	if err == nil {
		return
	}

	var apiErr *apierr.Error
	if errors.As(err, &apiErr) && apiErr.Code == invalidParamsCode {
		// -32500 is the catch-all refusal; the message says which one it is.
		// Both are retriable, on different clocks, so say which clock.
		reason := fmt.Sprintf("An additional IP cannot be released within 24h of being ordered. "+
			"The resource is kept in state and stays billed — re-run destroy after the lock "+
			"expires. The same applies to a replace (`-replace` / taint): it destroys before it "+
			"orders, so rotating an IP younger than 24h needs `create_before_destroy = true` or "+
			"a wait.\n\n  sweb vps ip list %s", billingID)
		if isVPSBusy(err) {
			reason = "Another operation is running on the VPS and SpaceWeb takes one at a time. " +
				"The resource is kept in state — re-run destroy once that operation finishes."
		}
		resp.Diagnostics.AddError("IP cannot be released yet",
			fmt.Sprintf("SpaceWeb refused to release %s from %s: %s\n\n%s", address, billingID, apiErr.Message, reason))
		return
	}
	resp.Diagnostics.AddError("Failed to release the IP", err.Error())
}

// ImportState adopts an address already on a VPS, addressed as
// "<billing_id>:<ip>" — the VPS alone is not enough, a VPS can hold several.
func (r *vpsIPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	billingID, address, ok := strings.Cut(req.ID, ":")
	if !ok || billingID == "" || address == "" {
		resp.Diagnostics.AddError("Invalid import id",
			fmt.Sprintf("expected \"<billing_id>:<ip>\" (e.g. login_vps_1:203.0.113.7), got %q", req.ID))
		return
	}

	addrs, err := r.addresses(ctx, billingID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the VPS IPs", err.Error())
		return
	}
	addr, found := addrs[address]
	if !found {
		resp.Diagnostics.AddError("IP not found on the VPS",
			fmt.Sprintf("%s is not among the IPs of %s — check `sweb vps ip list %s`.", address, billingID, billingID))
		return
	}

	set := func(name string, val any) {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), val)...)
	}
	set("id", billingID+":"+addr.IP)
	set("billing_id", billingID)
	set("ip", addr.IP)
	set("gateway", addr.Gateway)
	set("netmask", addr.Netmask)
}

// --- helpers ---

func (m *vpsIPModel) applyAddress(billingID string, a swebip.Address) {
	m.ID = types.StringValue(billingID + ":" + a.IP)
	m.BillingID = types.StringValue(billingID)
	m.IP = types.StringValue(a.IP)
	m.Gateway = types.StringValue(a.Gateway)
	m.Netmask = types.StringValue(a.Netmask)
}

// addresses returns the VPS's current public IPs, keyed by address.
func (r *vpsIPResource) addresses(ctx context.Context, billingID string) (map[string]swebip.Address, error) {
	info, err := r.client.IP.Info(ctx, billingID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]swebip.Address, len(info.IPs))
	for _, a := range info.IPs {
		out[a.IP] = a
	}
	return out, nil
}

// newAddress returns the VPS's first IP that was not in before, or nil.
func (r *vpsIPResource) newAddress(ctx context.Context, billingID string, before map[string]swebip.Address) (*swebip.Address, error) {
	addrs, err := r.addresses(ctx, billingID)
	if err != nil {
		return nil, err
	}
	for a, addr := range addrs {
		if _, existed := before[a]; !existed {
			return &addr, nil
		}
	}
	return nil, nil
}

// orderIP places the order for one additional IP, retrying while SpaceWeb reports
// another operation in flight on the VPS. A busy refusal means the order was not
// placed, but the list is re-read before each retry regardless: re-ordering on a
// false negative bills a second address that nothing in state points at.
func (r *vpsIPResource) orderIP(ctx context.Context, billingID string, before map[string]swebip.Address) error {
	for backoff := ipBusyBackoffFirst; ; backoff = min(backoff*2, ipBusyBackoffCap) {
		_, err := r.client.IP.Add(ctx, billingID, 1)
		if err == nil || !isVPSBusy(err) {
			return err
		}
		if sleepErr := sleepCtx(ctx, backoff); sleepErr != nil {
			return fmt.Errorf("the VPS stayed busy for the whole create timeout: %w", err)
		}
		if addr, aerr := r.newAddress(ctx, billingID, before); aerr == nil && addr != nil {
			return nil
		}
	}
}

// waitForNewAddress polls the VPS's IP list until an address that was not in
// before shows up (SpaceWeb assigns an ordered IP asynchronously), or the
// context's create timeout runs out.
func (r *vpsIPResource) waitForNewAddress(ctx context.Context, billingID string, before map[string]swebip.Address) (swebip.Address, error) {
	for {
		addr, err := r.newAddress(ctx, billingID, before)
		if err == nil && addr != nil {
			return *addr, nil
		}
		if sleepErr := sleepCtx(ctx, ipPollInterval); sleepErr != nil {
			if err != nil {
				return swebip.Address{}, err
			}
			return swebip.Address{}, fmt.Errorf("timed out waiting for the ordered IP to appear on %s", billingID)
		}
	}
}

// isVPSBusy reports whether err is SpaceWeb's "another operation is already
// running" refusal (-32500, "Выполняется другая операция"). The code alone does
// not say: it is the catch-all for every refused operation, the 24h IP lock
// included, so the message decides. An unrecognised -32500 is deliberately not
// busy — surfacing it at once beats retrying a permanent refusal for a timeout.
func isVPSBusy(err error) bool {
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != invalidParamsCode {
		return false
	}
	msg := strings.ToLower(apiErr.Message)
	return strings.Contains(msg, "друг") && strings.Contains(msg, "операц")
}

// sleepCtx waits for d, or returns the context's error if it finishes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
