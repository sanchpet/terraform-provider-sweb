package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/apierr"
	"github.com/sanchpet/sweb-go-sdk/vps"
)

// invalidParamsCode is the SpaceWeb JSON-RPC code for "invalid params /
// operation unavailable", which also covers the 24h post-create deletion lock.
const invalidParamsCode = -32500

// createMu serializes VPS creation across all sweb_vps resources in this provider
// process. SpaceWeb's create is a single-writer operation: it returns nothing
// usable, so the provider correlates the new node by diffing the VPS list before
// and after (listBillingIDs → Create → waitForNewVPS). Two creates running
// concurrently break that both ways — the correlation could adopt the other
// create's node, and the API itself rejects simultaneous create orders (-32000
// Internal Server Error). Terraform runs resources concurrently up to
// -parallelism; this mutex keeps each create's before→create→find window serial
// regardless, so consumers never have to pass -parallelism=1.
var createMu sync.Mutex

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
	// keep = "reuse the prior state value when the planned value is unknown". For computed
	// attributes that the only in-place op (rename) does not change, this keeps them out of
	// the plan (no "known after apply" noise); Read still refreshes them for drift each cycle.
	keepStr := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	keepBool := []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		Description: "A SpaceWeb VPS instance. alias (rename) and plan/cpu/ram/disk (resize via changePlan) update in place; category, distributive and datacenter force replacement. Disk can only grow — the API refuses shrinking it.",
		Attributes: map[string]schema.Attribute{
			// Configurator inputs. cpu/ram/disk update in place (resize); disk grow-only.
			"cpu": schema.Int64Attribute{
				Optional:    true,
				Description: "Configurator: CPU cores. Resolved to a plan via getConstructorPlanId. Updates in place (resize). Mutually exclusive with `plan`.",
			},
			"ram": schema.Int64Attribute{
				Optional:    true,
				Description: "Configurator: RAM in GB. Updates in place (resize). Mutually exclusive with `plan`.",
			},
			"disk": schema.Int64Attribute{
				Optional:    true,
				Description: "Configurator: disk in GB. Updates in place (resize); can only grow — the API refuses shrinking. Mutually exclusive with `plan`.",
			},
			"category": schema.Int64Attribute{
				Optional:      true,
				Description:   "Configurator: catalog category id (1=nvme, 2=hdd, 3=turbo). Defaults to 1 when the configurator is used. Changing the storage tier forces replacement. Mutually exclusive with `plan`.",
				PlanModifiers: replaceInt,
			},
			"plan": schema.Int64Attribute{
				Optional:    true,
				Description: "Ready-made plan id. Updates in place (resize via changePlan). Mutually exclusive with the configurator (`cpu`/`ram`/`disk`).",
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
				Required:    true,
				Description: "Human-facing name for the VPS. Updated in place (no replacement) via the API rename.",
			},
			"ssh_key": schema.StringAttribute{
				Optional:      true,
				Description:   "SSH public key content (the raw `ssh-ed25519 ...` string, not a key id) to inject at create. Create-only: not read back from the API; changing it forces replacement.",
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
				PlanModifiers: keepStr,
			},
			"billing_id": schema.StringAttribute{
				Computed:      true,
				Description:   "SpaceWeb service id (login_vps_N); the key used for delete and import.",
				PlanModifiers: keepStr,
			},
			"uid": schema.StringAttribute{
				Computed:      true,
				Description:   "Stable unique id of the VPS.",
				PlanModifiers: keepStr,
			},
			// name is NOT kept: it mirrors alias, so a rename legitimately changes it.
			"name": schema.StringAttribute{Computed: true, Description: "Effective name reported by the API."},
			"ip":   schema.StringAttribute{Computed: true, Description: "Primary IP address.", PlanModifiers: keepStr},
			"running": schema.BoolAttribute{
				Computed:      true,
				Description:   "Whether the VPS is running.",
				PlanModifiers: keepBool,
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true}),
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
	planID, err := r.resolvePlanID(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve configurator plan", err.Error())
		return
	}

	createReq := vps.CreateRequest{
		DistributiveID: int(plan.Distributive.ValueInt64()),
		VPSPlanID:      planID,
		Datacenter:     int(plan.Datacenter.ValueInt64()),
		Alias:          plan.Alias.ValueString(),
		SSHKey:         plan.SSHKey.ValueString(),
		IPCount:        int(plan.IPCount.ValueInt64()),
	}

	// The whole snapshot → create → correlate window must be serial: the List-diff
	// (before/after) that identifies the new node is only unambiguous when a single
	// create runs at a time (see createMu). Holding the lock through waitForNewVPS
	// also serializes the create orders the API can't take concurrently.
	var node vps.VPS
	ok := func() bool {
		createMu.Lock()
		defer createMu.Unlock()

		// Snapshot existing billing ids so we can detect the new node (Create's
		// response shape is untyped/unreliable — correlate via List-diff).
		before, err := r.listBillingIDs(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list VPS before create", err.Error())
			return false
		}
		if _, err := r.client.VPS.Create(ctx, createReq); err != nil {
			resp.Diagnostics.AddError("Failed to create VPS", err.Error())
			return false
		}
		// Poll until the new node appears and is running (or the timeout elapses).
		n, err := r.waitForNewVPS(ctx, before, createTimeout)
		if err != nil {
			resp.Diagnostics.AddError("VPS did not become ready", err.Error())
			return false
		}
		node = n
		return true
	}()
	if !ok {
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

// Update applies the in-place changes: alias (rename) and plan/cpu/ram/disk
// (resize via changePlan). category/distributive/datacenter are RequiresReplace,
// so Terraform only routes here for the in-place set. Disk grows only (the API
// refuses shrinking). A resize is async, so it waits until current_action idles.
func (r *vpsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vpsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	billingID := state.BillingID.ValueString()

	// alias — in-place label change (rename).
	if plan.Alias.ValueString() != state.Alias.ValueString() {
		if err := r.client.VPS.Rename(ctx, billingID, plan.Alias.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to rename VPS", err.Error())
			return
		}
	}

	// plan / cpu / ram / disk — in-place resize via changePlan.
	if resizeChanged(plan, state) {
		// Disk grows only: the API refuses shrinking (-32500 "Нельзя уменьшить
		// размер диска"). Catch it before mutating, with a clear message.
		if !plan.Disk.IsNull() && !state.Disk.IsNull() && plan.Disk.ValueInt64() < state.Disk.ValueInt64() {
			resp.Diagnostics.AddError("Disk cannot be shrunk",
				fmt.Sprintf("SpaceWeb refuses to reduce a VPS disk (current %d GB, requested %d GB). "+
					"Set disk to at least the current size.", state.Disk.ValueInt64(), plan.Disk.ValueInt64()))
			return
		}
		planID, err := r.resolvePlanID(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Failed to resolve target plan", err.Error())
			return
		}
		if err := r.client.VPS.ChangePlan(ctx, billingID, planID); err != nil {
			resp.Diagnostics.AddError("Failed to change plan", err.Error())
			return
		}
		// The resize is asynchronous (Modify → ExtIpAdd → …) while is_running stays
		// 1; wait until current_action settles before reading back.
		updateTimeout, diags := plan.Timeouts.Update(ctx, 15*time.Minute)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		waitCtx, cancel := context.WithTimeout(ctx, updateTimeout)
		defer cancel()
		if _, err := r.client.VPS.WaitForIdle(waitCtx, billingID, 10*time.Second, nil); err != nil {
			resp.Diagnostics.AddError("Resize did not settle", err.Error())
			return
		}
	}

	// Re-read so computed + input fields reflect the change (name follows alias;
	// cpu/ram/disk/plan follow the resize). ssh_key/timeouts stay from the plan.
	node, err := r.findByBillingID(ctx, billingID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read VPS after update", err.Error())
		return
	}
	if node == nil {
		resp.Diagnostics.AddError("VPS not found after update", fmt.Sprintf("no VPS with billing_id %q", billingID))
		return
	}
	r.applyAPIState(&plan, *node)
	refreshInputs(&plan, *node)
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

	var apiErr *apierr.Error
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
	set("plan", int64(node.PlanID))
	set("distributive", int64(node.OSDistrID))
	set("datacenter", atoiOr(node.DatacenterID, 0))
	set("alias", node.Name)
}

// --- helpers ---

// applyAPIState copies the server-reported fields of node into m. Create-only
// secrets (ssh_key) are intentionally left as-is (the API never returns them).
func (r *vpsResource) applyAPIState(m *vpsModel, node vps.VPS) {
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
func refreshInputs(m *vpsModel, node vps.VPS) {
	if !m.CPU.IsNull() {
		m.CPU = types.Int64Value(int64(node.CPU))
	}
	if !m.RAM.IsNull() {
		m.RAM = types.Int64Value(int64(node.RAM))
	}
	if !m.Disk.IsNull() {
		// index reports disk as a localized string ("30 ГБ"), not a numeric diskGb;
		// parse the leading GB so a configurator resource doesn't drift to 0.
		m.Disk = types.Int64Value(parseDiskGB(node.Disk))
	}
	// PlanID is now FlexInt (int64); keep the current value if the API didn't
	// report a plan (0), preserving atoiOr's old fallback semantics.
	if !m.Plan.IsNull() && node.PlanID != 0 {
		m.Plan = types.Int64Value(int64(node.PlanID))
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

// resolvePlanID returns the plan id to provision or resize to: the explicit
// `plan`, or a configurator plan resolved from cpu/ram/disk/category.
func (r *vpsResource) resolvePlanID(ctx context.Context, m vpsModel) (int, error) {
	if !m.Plan.IsNull() {
		return int(m.Plan.ValueInt64()), nil
	}
	category := int(m.Category.ValueInt64())
	if m.Category.IsNull() {
		category = 1 // NVMe
	}
	return r.client.VPS.GetConstructorPlanID(ctx,
		int(m.CPU.ValueInt64()), int(m.RAM.ValueInt64()), int(m.Disk.ValueInt64()), category)
}

// resizeChanged reports whether any resize-relevant input (plan, or the
// configurator cpu/ram/disk) differs between plan and state.
func resizeChanged(plan, state vpsModel) bool {
	return !plan.Plan.Equal(state.Plan) ||
		!plan.CPU.Equal(state.CPU) ||
		!plan.RAM.Equal(state.RAM) ||
		!plan.Disk.Equal(state.Disk)
}

// parseDiskGB extracts the integer GB from the API's localized disk string
// (e.g. "30 ГБ" → 30). index reports disk as this human string, not a numeric
// field. Configurator disks are in GB, so the leading integer is the size.
func parseDiskGB(disk string) int64 {
	var n int64
	seen := false
	for _, ch := range strings.TrimSpace(disk) {
		if ch < '0' || ch > '9' {
			break
		}
		n = n*10 + int64(ch-'0')
		seen = true
	}
	if !seen {
		return 0
	}
	return n
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

func (r *vpsResource) findByBillingID(ctx context.Context, billingID string) (*vps.VPS, error) {
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
func (r *vpsResource) waitForNewVPS(ctx context.Context, before map[string]struct{}, timeout time.Duration) (vps.VPS, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var last vps.VPS
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
			return vps.VPS{}, fmt.Errorf("timed out waiting for the new VPS to appear")
		case <-ticker.C:
		}
	}
}
