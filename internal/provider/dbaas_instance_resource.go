package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/dbaas"
)

var (
	_ resource.Resource                = (*dbaasInstanceResource)(nil)
	_ resource.ResourceWithImportState = (*dbaasInstanceResource)(nil)
)

// NewDBaaSInstanceResource is the resource factory registered with the provider.
func NewDBaaSInstanceResource() resource.Resource { return &dbaasInstanceResource{} }

// dbaasInstanceResource manages a managed-database (DBaaS) cluster on /dbaas
// (createInstance/index/editInstance/removeInstance). It mirrors sweb_vps: create
// returns nothing usable, so the new cluster is correlated by a List-diff on
// billing_id and creates are serialized behind the package createMu; provisioning
// and resize are async, so both poll List until currentAction idles.
//
// Plan sizing is a ready-made plan_id (updatable in place — a resize via
// editInstance) rather than a cpu/memory/storage/replicas configurator: the
// createInstance/editInstance payloads take a plan id directly, so plan_id keeps
// the resource a thin pass-through. Resolve an id first with the sweb_plan-style
// getConstructorPlanId if a configurator is ever needed.
type dbaasInstanceResource struct{ client *sweb.Client }

// dbaasInstanceModel is the Terraform state/plan model for a sweb_dbaas_instance.
type dbaasInstanceModel struct {
	// Inputs.
	EngineType    types.String `tfsdk:"engine_type"`
	EngineVersion types.String `tfsdk:"engine_version"`
	PlanID        types.Int64  `tfsdk:"plan_id"`
	DisplayName   types.String `tfsdk:"display_name"`
	Users         []dbaasUser  `tfsdk:"user"`

	// Computed.
	ID        types.String `tfsdk:"id"` // = billing_id
	BillingID types.String `tfsdk:"billing_id"`
	Status    types.String `tfsdk:"status"`
	IP        types.String `tfsdk:"ip"`
	Endpoints types.List   `tfsdk:"endpoints"`
	Engine    types.String `tfsdk:"engine"`
	Active    types.Bool   `tfsdk:"active"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// dbaasUser is one cluster user block. password is write-only: the API never
// reports it back, and its edit semantics are password-present = create/set,
// password-absent = keep, user missing from the list = remove.
type dbaasUser struct {
	Name     types.String `tfsdk:"name"`
	Password types.String `tfsdk:"password"`
}

func (r *dbaasInstanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dbaas_instance"
}

func (r *dbaasInstanceResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keepStr := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		Description: "A SpaceWeb managed-database (DBaaS) cluster. display_name, plan_id (resize) and " +
			"the user set update in place via editInstance; engine_type and engine_version force replacement. " +
			"Identity is billing_id — the delete and import key.",
		Attributes: map[string]schema.Attribute{
			"engine_type": schema.StringAttribute{
				Required:      true,
				Description:   "DBMS engine type (e.g. PostgreSQL, MySQL). Forces replacement.",
				PlanModifiers: replaceStr,
			},
			"engine_version": schema.StringAttribute{
				Required:      true,
				Description:   "DBMS engine version (e.g. \"16\"). Forces replacement.",
				PlanModifiers: replaceStr,
			},
			"plan_id": schema.Int64Attribute{
				Required:    true,
				Description: "Ready-made DBaaS tariff id. Updates in place (resize via editInstance).",
			},
			"display_name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Human-facing cluster name. Updated in place via editInstance.",
			},

			// Computed.
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Terraform identifier — equals billing_id.",
				PlanModifiers: keepStr,
			},
			"billing_id": schema.StringAttribute{
				Computed:      true,
				Description:   "SpaceWeb service id; the key used for delete and import.",
				PlanModifiers: keepStr,
			},
			"status": schema.StringAttribute{Computed: true, Description: "Cluster status reported by the API."},
			"ip":     schema.StringAttribute{Computed: true, Description: "Primary connection endpoint (\"ip:port\").", PlanModifiers: keepStr},
			"endpoints": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Connection endpoints (\"type=ip:port\"; \"rw\" always present, \"ro\" when read replicas exist).",
			},
			"engine": schema.StringAttribute{Computed: true, Description: "Effective engine string reported by the API.", PlanModifiers: keepStr},
			"active": schema.BoolAttribute{Computed: true, Description: "Whether the cluster is active."},
		},
		Blocks: map[string]schema.Block{
			"user": schema.ListNestedBlock{
				Description: "A cluster user. At least one is required at create. Removing a block deletes " +
					"the user; changing a password re-sets it. Passwords are write-only (never read back).",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:    true,
							Description: "User name.",
						},
						"password": schema.StringAttribute{
							Required:    true,
							Sensitive:   true,
							Description: "User password. Write-only: not read back, so it is ignored on import and drift.",
						},
					},
				},
			},
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true}),
		},
	}
}

func (r *dbaasInstanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *dbaasInstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dbaasInstanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := dbaas.CreateInstanceRequest{
		EngineType:    plan.EngineType.ValueString(),
		EngineVersion: plan.EngineVersion.ValueString(),
		Users:         credentials(plan.Users), // full password set at create
		PlanID:        int(plan.PlanID.ValueInt64()),
		DisplayName:   plan.DisplayName.ValueString(),
	}

	// The whole snapshot → create → correlate window must be serial: the List-diff
	// (before/after) that identifies the new cluster is only unambiguous when a
	// single create runs at a time (see createMu, shared with sweb_vps). Holding the
	// lock through the poll also serializes the create orders the API can't take
	// concurrently.
	var inst dbaas.Instance
	ok := func() bool {
		createMu.Lock()
		defer createMu.Unlock()

		before, err := r.listBillingIDs(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list DBaaS instances before create", err.Error())
			return false
		}
		if _, err := r.client.DBaaS.CreateInstance(ctx, createReq); err != nil {
			resp.Diagnostics.AddError("Failed to create DBaaS instance", err.Error())
			return false
		}
		n, err := r.waitForNewInstance(ctx, before, createTimeout)
		if err != nil {
			resp.Diagnostics.AddError("DBaaS instance did not become ready", err.Error())
			return false
		}
		inst = n
		return true
	}()
	if !ok {
		return
	}

	r.applyAPIState(ctx, &plan, inst, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dbaasInstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dbaasInstanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inst, err := r.findByBillingID(ctx, state.BillingID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read DBaaS instance", err.Error())
		return
	}
	if inst == nil {
		resp.State.RemoveResource(ctx) // gone server-side → drop from state
		return
	}

	r.applyAPIState(ctx, &state, *inst, &resp.Diagnostics)
	// plan_id is the one updatable input the API echoes back cleanly (Plan.ID);
	// engine_type/engine_version are ForceNew and not separately reported, so their
	// drift is not tracked (they never change without a replacement).
	if inst.Plan.ID != 0 {
		state.PlanID = types.Int64Value(int64(inst.Plan.ID))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update applies the in-place changes via editInstance: display_name, plan_id
// (resize), and the user set. engine_type/engine_version are RequiresReplace, so
// Terraform only routes here for the in-place set. The edit is async, so it polls
// until currentAction idles.
func (r *dbaasInstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dbaasInstanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	billingID := state.BillingID.ValueString()

	editReq := dbaas.EditInstanceRequest{
		BillingID:   billingID,
		PlanID:      int(plan.PlanID.ValueInt64()),
		DisplayName: plan.DisplayName.ValueString(),
		Users:       editUsers(plan.Users, state.Users),
	}
	if err := r.client.DBaaS.EditInstance(ctx, editReq); err != nil {
		resp.Diagnostics.AddError("Failed to edit DBaaS instance", err.Error())
		return
	}

	updateTimeout, diags := plan.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	inst, err := r.waitForIdle(ctx, billingID, updateTimeout)
	if err != nil {
		resp.Diagnostics.AddError("DBaaS edit did not settle", err.Error())
		return
	}

	r.applyAPIState(ctx, &plan, inst, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dbaasInstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dbaasInstanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.DBaaS.RemoveInstance(ctx, state.BillingID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete DBaaS instance", err.Error())
	}
}

// ImportState imports by billing_id. User passwords are write-only (never
// API-reported), so they are left to config and ignored on import verification.
func (r *dbaasInstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	inst, err := r.findByBillingID(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to import DBaaS instance", err.Error())
		return
	}
	if inst == nil {
		resp.Diagnostics.AddError("DBaaS instance not found", fmt.Sprintf("no instance with billing_id %q", req.ID))
		return
	}

	set := func(name string, val any) {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), val)...)
	}
	set("id", inst.BillingID)
	set("billing_id", inst.BillingID)
	set("engine_type", engineType(inst.Engine, inst.Engine))
	set("engine_version", "")
	set("plan_id", int64(inst.Plan.ID))
	set("display_name", inst.DisplayName)
	set("status", inst.Status)
	set("ip", inst.IP)
	set("engine", inst.Engine)
	set("active", inst.Active)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("endpoints"), endpointStrings(inst.Endpoints))...)
}

// --- helpers ---

// applyAPIState copies the server-reported fields of inst into m. User passwords
// are write-only, so the user set is left as-is (kept from plan/state/config).
func (r *dbaasInstanceResource) applyAPIState(ctx context.Context, m *dbaasInstanceModel, inst dbaas.Instance, diags *diag.Diagnostics) {
	m.ID = types.StringValue(inst.BillingID)
	m.BillingID = types.StringValue(inst.BillingID)
	m.Status = types.StringValue(inst.Status)
	m.IP = types.StringValue(inst.IP)
	m.Engine = types.StringValue(inst.Engine)
	m.Active = types.BoolValue(inst.Active)
	if inst.DisplayName != "" {
		m.DisplayName = types.StringValue(inst.DisplayName)
	}
	eps, d := types.ListValueFrom(ctx, types.StringType, endpointStrings(inst.Endpoints))
	diags.Append(d...)
	m.Endpoints = eps
}

func (r *dbaasInstanceResource) listBillingIDs(ctx context.Context) (map[string]struct{}, error) {
	idx, err := r.client.DBaaS.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(idx.Instances))
	for _, inst := range idx.Instances {
		ids[inst.BillingID] = struct{}{}
	}
	return ids, nil
}

func (r *dbaasInstanceResource) findByBillingID(ctx context.Context, billingID string) (*dbaas.Instance, error) {
	idx, err := r.client.DBaaS.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range idx.Instances {
		if idx.Instances[i].BillingID == billingID {
			return &idx.Instances[i], nil
		}
	}
	return nil, nil
}

// waitForNewInstance polls List until a billing id not present in `before`
// appears and the cluster is idle (currentAction == ""), or ctx/timeout expires.
func (r *dbaasInstanceResource) waitForNewInstance(ctx context.Context, before map[string]struct{}, timeout time.Duration) (dbaas.Instance, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var last dbaas.Instance
	found := false
	for {
		idx, err := r.client.DBaaS.List(ctx)
		if err == nil {
			for _, inst := range idx.Instances {
				if _, existed := before[inst.BillingID]; existed {
					continue
				}
				last, found = inst, true
				if inst.CurrentAction == "" {
					return inst, nil
				}
			}
		}

		select {
		case <-ctx.Done():
			if found {
				return last, nil // appeared but not yet idle — return what we have
			}
			return dbaas.Instance{}, fmt.Errorf("timed out waiting for the new DBaaS instance to appear")
		case <-ticker.C:
		}
	}
}

// waitForIdle polls List for billingID until it reports idle (currentAction ==
// ""), or ctx/timeout expires.
func (r *dbaasInstanceResource) waitForIdle(ctx context.Context, billingID string, timeout time.Duration) (dbaas.Instance, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var last dbaas.Instance
	found := false
	for {
		inst, err := r.findByBillingID(ctx, billingID)
		if err == nil && inst != nil {
			last, found = *inst, true
			if inst.CurrentAction == "" {
				return *inst, nil
			}
		}

		select {
		case <-ctx.Done():
			if found {
				return last, nil
			}
			return dbaas.Instance{}, fmt.Errorf("timed out waiting for DBaaS instance %q to settle", billingID)
		case <-ticker.C:
		}
	}
}

// credentials maps the resource user blocks to SDK create credentials (every
// user carries its password at create).
func credentials(users []dbaasUser) []dbaas.UserCredentials {
	out := make([]dbaas.UserCredentials, 0, len(users))
	for _, u := range users {
		out = append(out, dbaas.UserCredentials{Name: u.Name.ValueString(), Password: u.Password.ValueString()})
	}
	return out
}

// editUsers builds the editInstance user list honoring the API's edit semantics:
// a user new to the plan, or one whose password changed, is sent WITH its
// password (created / re-set); an unchanged user is sent WITHOUT a password
// (kept); a user in state but absent from the plan is omitted (removed).
func editUsers(plan, state []dbaasUser) []dbaas.UserCredentials {
	prior := make(map[string]string, len(state))
	for _, u := range state {
		prior[u.Name.ValueString()] = u.Password.ValueString()
	}
	out := make([]dbaas.UserCredentials, 0, len(plan))
	for _, u := range plan {
		name, pass := u.Name.ValueString(), u.Password.ValueString()
		if old, existed := prior[name]; existed && old == pass {
			out = append(out, dbaas.UserCredentials{Name: name}) // unchanged → keep
			continue
		}
		out = append(out, dbaas.UserCredentials{Name: name, Password: pass}) // new or changed → set
	}
	return out
}

// endpointStrings renders the cluster endpoints as "type=ip:port" strings for the
// computed list attribute.
func endpointStrings(eps []dbaas.Endpoint) []string {
	out := make([]string, 0, len(eps))
	for _, e := range eps {
		out = append(out, fmt.Sprintf("%s=%s:%d", e.Type, e.IP, int64(e.Port)))
	}
	return out
}

// engineType derives the engine_type input from the API's engine string, falling
// back to the known value: index reports a single `engine` field, and the create
// input engine_type is not echoed back separately.
func engineType(engine, fallback string) string {
	if engine != "" {
		return engine
	}
	return fallback
}
