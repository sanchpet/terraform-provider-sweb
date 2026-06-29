package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

// invalidParamsCode is the SpaceWeb JSON-RPC code for "invalid params /
// operation unavailable", which also covers the 24h post-create deletion lock.
const invalidParamsCode = -32500

var (
	_ resource.Resource                     = (*vpsResource)(nil)
	_ resource.ResourceWithImportState      = (*vpsResource)(nil)
	_ resource.ResourceWithConfigValidators = (*vpsResource)(nil)
)

// NewVPSResource is the resource factory registered with the provider.
func NewVPSResource() resource.Resource { return &vpsResource{} }

type vpsResource struct{ client *sweb.Client }

// vpsModel is the Terraform state/plan model for a sweb_vps.
type vpsModel struct {
	// Inputs — configurator (mutually exclusive with Plan).
	CPU      types.Int64 `tfsdk:"cpu"`
	RAM      types.Int64 `tfsdk:"ram"`
	Disk     types.Int64 `tfsdk:"disk"`
	Category types.Int64 `tfsdk:"category"`
	// Input — ready-made plan (mutually exclusive with the configurator).
	Plan types.Int64 `tfsdk:"plan"`

	// Common inputs.
	Distributive types.Int64  `tfsdk:"distributive"`
	Datacenter   types.Int64  `tfsdk:"datacenter"`
	Alias        types.String `tfsdk:"alias"`
	SSHKey       types.String `tfsdk:"ssh_key"`
	IPCount      types.Int64  `tfsdk:"ip_count"`

	// Computed.
	ID        types.String `tfsdk:"id"` // = billing_id
	BillingID types.String `tfsdk:"billing_id"`
	UID       types.String `tfsdk:"uid"`
	Name      types.String `tfsdk:"name"`
	IP        types.String `tfsdk:"ip"`
	Running   types.Bool   `tfsdk:"running"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (r *vpsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vps"
}

func (r *vpsResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	replaceStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: "A SpaceWeb VPS instance. v1 recreates on any input change (no in-place resize/rename yet).",
		Attributes: map[string]schema.Attribute{
			// Configurator inputs.
			"cpu": schema.Int64Attribute{
				Optional:      true,
				Description:   "Configurator: CPU cores. Resolved to a plan via getConstructorPlanId. Mutually exclusive with `plan`.",
				PlanModifiers: replaceInt,
			},
			"ram": schema.Int64Attribute{
				Optional:      true,
				Description:   "Configurator: RAM in GB. Mutually exclusive with `plan`.",
				PlanModifiers: replaceInt,
			},
			"disk": schema.Int64Attribute{
				Optional:      true,
				Description:   "Configurator: disk in GB. Mutually exclusive with `plan`.",
				PlanModifiers: replaceInt,
			},
			"category": schema.Int64Attribute{
				Optional:      true,
				Description:   "Configurator: catalog category id (1=nvme, 2=hdd, 3=turbo). Defaults to 1 when the configurator is used. Mutually exclusive with `plan`.",
				PlanModifiers: replaceInt,
			},
			"plan": schema.Int64Attribute{
				Optional:      true,
				Description:   "Ready-made plan id. Mutually exclusive with the configurator (`cpu`/`ram`/`disk`).",
				PlanModifiers: replaceInt,
			},

			// Common inputs.
			"distributive": schema.Int64Attribute{
				Required:      true,
				Description:   "OS distributive id (e.g. 164=debian-13, 122=ubuntu-24.04).",
				PlanModifiers: replaceInt,
			},
			"datacenter": schema.Int64Attribute{
				Required:      true,
				Description:   "Datacenter id (1=spb, 2=msk, 3=ams).",
				PlanModifiers: replaceInt,
			},
			"alias": schema.StringAttribute{
				Required:      true,
				Description:   "Human-facing name for the VPS.",
				PlanModifiers: replaceStr,
			},
			"ssh_key": schema.StringAttribute{
				Optional:      true,
				Description:   "SSH public key id to inject at create. Create-only: not read back from the API.",
				PlanModifiers: replaceStr,
			},
			"ip_count": schema.Int64Attribute{
				Optional:      true,
				Description:   "Number of IPs to order. Create-only.",
				PlanModifiers: replaceInt,
			},

			// Computed.
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Terraform identifier — equals billing_id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"billing_id": schema.StringAttribute{
				Computed:      true,
				Description:   "SpaceWeb service id (login_vps_N); the key used for delete and import.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"uid": schema.StringAttribute{
				Computed:    true,
				Description: "Stable unique id of the VPS.",
			},
			"name":    schema.StringAttribute{Computed: true, Description: "Effective name reported by the API."},
			"ip":      schema.StringAttribute{Computed: true, Description: "Primary IP address."},
			"running": schema.BoolAttribute{Computed: true, Description: "Whether the VPS is running."},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *vpsResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		// Exactly one provisioning mode: a ready-made plan, or the configurator (cpu).
		resourcevalidator.ExactlyOneOf(
			path.MatchRoot("plan"),
			path.MatchRoot("cpu"),
		),
		// When the configurator is used, ram and disk go with cpu.
		resourcevalidator.RequiredTogether(
			path.MatchRoot("cpu"),
			path.MatchRoot("ram"),
			path.MatchRoot("disk"),
		),
	}
}

func (r *vpsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*sweb.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *sweb.Client, got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *vpsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve the plan id: explicit `plan`, or via the configurator.
	planID := int(plan.Plan.ValueInt64())
	if plan.Plan.IsNull() {
		category := int(plan.Category.ValueInt64())
		if plan.Category.IsNull() {
			category = 1
		}
		id, err := r.client.VPS.GetConstructorPlanID(ctx,
			int(plan.CPU.ValueInt64()), int(plan.RAM.ValueInt64()), int(plan.Disk.ValueInt64()), category)
		if err != nil {
			resp.Diagnostics.AddError("Failed to resolve configurator plan", err.Error())
			return
		}
		planID = id
	}

	// Snapshot existing billing ids so we can detect the new node (Create's
	// response shape is untyped/unreliable — correlate via List-diff).
	before, err := r.listBillingIDs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list VPS before create", err.Error())
		return
	}

	_, err = r.client.VPS.Create(ctx, sweb.CreateVPSRequest{
		DistributiveID: int(plan.Distributive.ValueInt64()),
		VPSPlanID:      planID,
		Datacenter:     int(plan.Datacenter.ValueInt64()),
		Alias:          plan.Alias.ValueString(),
		SSHKey:         plan.SSHKey.ValueString(),
		IPCount:        int(plan.IPCount.ValueInt64()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VPS", err.Error())
		return
	}

	// Poll until the new node appears and is running (or the timeout elapses).
	node, err := r.waitForNewVPS(ctx, before, createTimeout)
	if err != nil {
		resp.Diagnostics.AddError("VPS did not become ready", err.Error())
		return
	}

	r.applyAPIState(&plan, node)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	node, err := r.findByBillingID(ctx, state.BillingID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read VPS", err.Error())
		return
	}
	if node == nil {
		resp.State.RemoveResource(ctx) // gone server-side → drop from state
		return
	}

	r.applyAPIState(&state, *node)
	refreshInputs(&state, *node) // mode-aware drift: only fields already managed
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update should never run: every input is RequiresReplace in v1. Implemented to
// satisfy the interface; a no-op that re-persists the plan.
func (r *vpsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vpsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	billingID := state.BillingID.ValueString()
	_, err := r.client.VPS.Remove(ctx, billingID)
	if err == nil {
		return
	}

	var apiErr *sweb.Error
	if errors.As(err, &apiErr) && apiErr.Code == invalidParamsCode {
		resp.Diagnostics.AddError(
			"VPS cannot be deleted yet",
			fmt.Sprintf("SpaceWeb refused to remove %s: %s\n\n"+
				"A freshly created VPS is locked from deletion for 24h. The resource is kept "+
				"in state — re-run destroy after the lock expires.", billingID, apiErr.Message),
		)
		return
	}
	resp.Diagnostics.AddError("Failed to delete VPS", err.Error())
}

func (r *vpsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import by billing_id. The API exposes a resolved plan_id for every node
	// (even configurator ones), so we reconstruct a valid, round-trippable config
	// in plan-mode. Create-only secrets (ssh_key) are not recoverable from the API.
	node, err := r.findByBillingID(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to import VPS", err.Error())
		return
	}
	if node == nil {
		resp.Diagnostics.AddError("VPS not found", fmt.Sprintf("no VPS with billing_id %q", req.ID))
		return
	}

	// resp.State is pre-typed with the resource schema (all-null), so we set
	// attributes individually and leave timeouts as its schema-typed null.
	set := func(name string, val any) {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), val)...)
	}
	set("id", node.BillingID)
	set("billing_id", node.BillingID)
	set("uid", node.UID)
	set("name", node.Name)
	set("ip", node.IP)
	set("running", node.IsRunning == 1)
	set("plan", atoiOr(node.PlanID, 0))
	set("distributive", node.OSDistrID)
	set("datacenter", atoiOr(node.DatacenterID, 0))
	set("alias", node.Name)
}

// --- helpers ---

// applyAPIState copies the server-reported fields of node into m. Create-only
// secrets (ssh_key) are intentionally left as-is (the API never returns them).
func (r *vpsResource) applyAPIState(m *vpsModel, node sweb.VPS) {
	m.ID = types.StringValue(node.BillingID)
	m.BillingID = types.StringValue(node.BillingID)
	m.UID = types.StringValue(node.UID)
	m.Name = types.StringValue(node.Name)
	m.IP = types.StringValue(node.IP)
	m.Running = types.BoolValue(node.IsRunning == 1)
}

// refreshInputs syncs managed input fields from the API for drift detection.
// It only touches fields already set in state (mode-aware): a configurator
// resource refreshes cpu/ram/disk; a plan resource refreshes plan; neither
// adopts the other's fields. `category` is configurator-only and not reported by
// the API, so it is never refreshed (drift on it is not detected).
func refreshInputs(m *vpsModel, node sweb.VPS) {
	if !m.CPU.IsNull() {
		m.CPU = types.Int64Value(int64(node.CPU))
	}
	if !m.RAM.IsNull() {
		m.RAM = types.Int64Value(int64(node.RAM))
	}
	if !m.Disk.IsNull() {
		m.Disk = types.Int64Value(int64(node.DiskGB))
	}
	if !m.Plan.IsNull() {
		m.Plan = types.Int64Value(atoiOr(node.PlanID, m.Plan.ValueInt64()))
	}
	if !m.Distributive.IsNull() {
		m.Distributive = types.Int64Value(int64(node.OSDistrID))
	}
	if !m.Datacenter.IsNull() {
		m.Datacenter = types.Int64Value(atoiOr(node.DatacenterID, m.Datacenter.ValueInt64()))
	}
	if !m.Alias.IsNull() {
		m.Alias = types.StringValue(node.Name)
	}
}

// atoiOr parses s as an int64, returning fallback on failure (the API sometimes
// reports numeric ids as strings).
func atoiOr(s string, fallback int64) int64 {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return fallback
}

func (r *vpsResource) listBillingIDs(ctx context.Context) (map[string]struct{}, error) {
	list, err := r.client.VPS.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(list))
	for _, v := range list {
		ids[v.BillingID] = struct{}{}
	}
	return ids, nil
}

func (r *vpsResource) findByBillingID(ctx context.Context, billingID string) (*sweb.VPS, error) {
	list, err := r.client.VPS.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].BillingID == billingID {
			return &list[i], nil
		}
	}
	return nil, nil
}

// waitForNewVPS polls List until a billing id not present in `before` appears
// and the node reports running, or ctx/timeout is exhausted.
func (r *vpsResource) waitForNewVPS(ctx context.Context, before map[string]struct{}, timeout time.Duration) (sweb.VPS, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var last sweb.VPS
	found := false
	for {
		list, err := r.client.VPS.List(ctx)
		if err == nil {
			for _, v := range list {
				if _, existed := before[v.BillingID]; existed {
					continue
				}
				last, found = v, true
				if v.IsRunning == 1 {
					return v, nil
				}
			}
		}

		select {
		case <-ctx.Done():
			if found {
				return last, nil // appeared but not yet "running" — return what we have
			}
			return sweb.VPS{}, fmt.Errorf("timed out waiting for the new VPS to appear")
		case <-ticker.C:
		}
	}
}
