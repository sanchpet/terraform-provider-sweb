package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

var (
	_ resource.Resource                = (*localNetworkResource)(nil)
	_ resource.ResourceWithImportState = (*localNetworkResource)(nil)
)

// NewLocalNetworkResource is the resource factory registered with the provider.
func NewLocalNetworkResource() resource.Resource { return &localNetworkResource{} }

// localNetworkResource attaches an EXISTING VPS to the account private (local)
// network via the SDK IP service (addLocal/removeLocal). Declarative — no VPS
// re-create. It is a separate resource (not an attribute of sweb_vps) because the
// API models it independently and it applies to imported/pre-existing nodes.
type localNetworkResource struct{ client *sweb.Client }

type localNetworkModel struct {
	BillingID types.String   `tfsdk:"billing_id"`
	ID        types.String   `tfsdk:"id"`
	LocalIP   types.String   `tfsdk:"local_ip"`
	Mask      types.String   `tfsdk:"mask"`
	MAC       types.String   `tfsdk:"mac"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func (r *localNetworkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vps_local_network"
}

func (r *localNetworkResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Attaches an existing VPS to the account private (local) network (addLocal/" +
			"removeLocal on /vps/ip). Declarative, no VPS re-create. SpaceWeb assigns the local IP; " +
			"the guest OS still needs the private NIC configured (netplan/ifcfg) with it.",
		Attributes: map[string]schema.Attribute{
			"billing_id": schema.StringAttribute{
				Required:      true,
				Description:   "VPS service id (login_vps_N) to attach. Changing it re-attaches a different VPS.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (equals billing_id).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"local_ip": schema.StringAttribute{Computed: true, Description: "Assigned local (private) IP."},
			"mask":     schema.StringAttribute{Computed: true, Description: "Local subnet mask."},
			"mac":      schema.StringAttribute{Computed: true, Description: "Local interface MAC address."},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *localNetworkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *localNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan localNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	billingID := plan.BillingID.ValueString()

	createTimeout, diags := plan.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.IP.AddLocal(ctx, billingID); err != nil {
		resp.Diagnostics.AddError("Failed to attach VPS to the local network", err.Error())
		return
	}

	// The attach can be asynchronous — poll until the local IP is assigned.
	waitCtx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()
	lip, err := r.client.IP.WaitForLocalIP(waitCtx, billingID, 10*time.Second)
	if err != nil {
		resp.Diagnostics.AddError("Local IP was not assigned", err.Error())
		return
	}

	plan.ID = types.StringValue(billingID)
	plan.LocalIP = types.StringValue(lip.IP)
	plan.Mask = types.StringValue(lip.Mask)
	plan.MAC = types.StringValue(lip.MAC)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *localNetworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state localNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	info, err := r.client.IP.Info(ctx, state.BillingID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read local network", err.Error())
		return
	}
	if len(info.LocalIP) == 0 {
		resp.State.RemoveResource(ctx) // detached server-side → drop from state
		return
	}
	lip := info.LocalIP[0]
	state.ID = types.StringValue(state.BillingID.ValueString())
	state.LocalIP = types.StringValue(lip.IP)
	state.Mask = types.StringValue(lip.Mask)
	state.MAC = types.StringValue(lip.MAC)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update has no mutable attributes (billing_id forces replacement, the rest are
// computed) — it only runs for timeouts-block changes. Carry the computed values
// forward from state so they don't go unknown.
func (r *localNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state localNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	plan.LocalIP = state.LocalIP
	plan.Mask = state.Mask
	plan.MAC = state.MAC
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *localNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state localNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.IP.RemoveLocal(ctx, state.BillingID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to detach VPS from the local network", err.Error())
		return
	}
}

func (r *localNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("billing_id"), req, resp)
}
