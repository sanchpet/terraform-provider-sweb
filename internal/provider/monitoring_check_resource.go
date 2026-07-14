package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/monitoring/checks"
)

var (
	_ resource.Resource                = (*monitoringCheckResource)(nil)
	_ resource.ResourceWithImportState = (*monitoringCheckResource)(nil)
)

// checkTypeIDs maps the human check type to the API's numeric type id
// (Spec.Type: 1 Ping, 2 Http, 3 Port).
var checkTypeIDs = map[string]int{"ping": 1, "http": 2, "port": 3}

// NewMonitoringCheckResource is the resource factory registered with the provider.
func NewMonitoringCheckResource() resource.Resource { return &monitoringCheckResource{} }

// monitoringCheckResource manages one monitoring check (create/edit/activate/
// deactivate/remove + index on /monitoring/checks).
//
// The API's create returns no usable id (only a 1/0 success sentinel), so the
// new check is correlated by a List-diff: the check id present after create but
// not before. Identity is that numeric `id`. A plain before/after diff is sound
// here without a create mutex — creates on this endpoint don't share the VPS
// single-writer constraint, and the mock imposes none. `type` cannot be changed
// through edit (edit is keyed by id and drops the type param), so it forces
// replacement; target/name/interval/contacts/port/ssl/keywords update in place.
// `enabled` is toggled through activate/deactivate.
type monitoringCheckResource struct{ client *sweb.Client }

type monitoringCheckModel struct {
	Type        types.String `tfsdk:"type"`
	Target      types.String `tfsdk:"target"`
	Name        types.String `tfsdk:"name"`
	Interval    types.Int64  `tfsdk:"interval"`
	ContactIDs  types.List   `tfsdk:"contact_ids"`
	Port        types.Int64  `tfsdk:"port"`
	SSL         types.Bool   `tfsdk:"ssl"`
	Keywords    types.List   `tfsdk:"keywords"`
	KeywordMode types.Int64  `tfsdk:"keyword_mode"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	ID          types.String `tfsdk:"id"`
}

func (r *monitoringCheckResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitoring_check"
}

func (r *monitoringCheckResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages one monitoring check (create/edit/activate/deactivate/remove on /monitoring/checks). " +
			"The API's create returns no id, so the new check is correlated by a List-diff on the check ids. " +
			"`type` forces replacement (edit is keyed by id, not type); target, name, interval, contacts, port, " +
			"ssl and keywords update in place; `enabled` is toggled via activate/deactivate.",
		Attributes: map[string]schema.Attribute{
			"type": schema.StringAttribute{
				Required: true,
				Description: "Check type: one of ping, http, port. Forces replacement — the API's edit is keyed " +
					"by id and cannot change a check's type.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf("ping", "http", "port")},
			},
			"target": schema.StringAttribute{
				Required:    true,
				Description: "URL or IP to check. Updated in place via edit.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name for the check. Updated in place via edit.",
			},
			"interval": schema.Int64Attribute{
				Required:    true,
				Description: "Interval id (see the account's available intervals). Updated in place via edit.",
			},
			"contact_ids": schema.ListAttribute{
				Required:    true,
				ElementType: types.Int64Type,
				Description: "Ids of the monitoring contacts to notify. Updated in place via edit.",
			},
			"port": schema.Int64Attribute{
				Optional:    true,
				Description: "Port to check (Port checks only). Omitted when 0/unset.",
			},
			"ssl": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether the HTTP check uses SSL (Http checks only).",
			},
			"keywords": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Keywords to match in the response body (Http checks only).",
			},
			"keyword_mode": schema.Int64Attribute{
				Optional:    true,
				Description: "Keyword-match mode id (Http checks only). Omitted when 0/unset.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the check is active. Toggled in place via activate/deactivate.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Numeric check id assigned by the API (the delete/import key).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *monitoringCheckResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *monitoringCheckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan monitoringCheckModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, diags := r.buildSpec(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create returns no id (1/0 sentinel only): snapshot the existing ids, create,
	// then correlate the new check as the id present after but not before.
	before, err := r.listCheckIDs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list checks before create", err.Error())
		return
	}
	if err := r.client.MonitoringChecks.Create(ctx, spec); err != nil {
		resp.Diagnostics.AddError("Failed to create monitoring check", err.Error())
		return
	}
	check, err := r.findNewCheck(ctx, before)
	if err != nil {
		resp.Diagnostics.AddError("Failed to correlate the new check", err.Error())
		return
	}

	// A new check is created active by default; deactivate it if the plan asked for
	// disabled. (activate is idempotent, so only the deactivate branch is needed.)
	if !plan.Enabled.ValueBool() {
		id, _ := strconv.Atoi(check.ID)
		if err := r.client.MonitoringChecks.Deactivate(ctx, id); err != nil {
			resp.Diagnostics.AddError("Failed to disable monitoring check", err.Error())
			return
		}
		check.Status = false
	}

	r.applyRemote(&plan, check)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *monitoringCheckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state monitoringCheckModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	check, found, err := r.findByID(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read monitoring checks", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx) // removed outside Terraform
		return
	}
	r.applyRemote(&state, check)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *monitoringCheckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state monitoringCheckModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid check id in state", err.Error())
		return
	}

	// type forces replacement, so it never reaches Update. Rebuild the spec from the
	// plan and edit in place; edit ignores the (unchanged) type.
	spec, diags := r.buildSpec(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.MonitoringChecks.Edit(ctx, id, spec); err != nil {
		resp.Diagnostics.AddError("Failed to update monitoring check", err.Error())
		return
	}

	// enabled is not part of the edit payload — toggle it separately when it changed.
	if !plan.Enabled.Equal(state.Enabled) {
		if err := r.toggleEnabled(ctx, id, plan.Enabled.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Failed to toggle monitoring check", err.Error())
			return
		}
	}

	check, found, err := r.findByID(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read check after update", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Check missing after update", fmt.Sprintf("no check with id %q", state.ID.ValueString()))
		return
	}
	r.applyRemote(&plan, check)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *monitoringCheckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state monitoringCheckModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid check id in state", err.Error())
		return
	}
	if err := r.client.MonitoringChecks.Remove(ctx, id); err != nil {
		resp.Diagnostics.AddError("Failed to delete monitoring check", err.Error())
		return
	}
}

// ImportState accepts the numeric check id. Read then fills the remaining fields.
func (r *monitoringCheckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := strconv.Atoi(req.ID); err != nil {
		resp.Diagnostics.AddError("Invalid import id", "expected a numeric check id, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// --- helpers ---

// buildSpec assembles the create/edit payload from the plan model.
func (r *monitoringCheckResource) buildSpec(ctx context.Context, m monitoringCheckModel) (checks.Spec, diag.Diagnostics) {
	var diags diag.Diagnostics

	var contactIDs []int
	if !m.ContactIDs.IsNull() && !m.ContactIDs.IsUnknown() {
		diags.Append(m.ContactIDs.ElementsAs(ctx, &contactIDs, false)...)
	}
	var keywords []string
	if !m.Keywords.IsNull() && !m.Keywords.IsUnknown() {
		diags.Append(m.Keywords.ElementsAs(ctx, &keywords, false)...)
	}

	return checks.Spec{
		Type:        checkTypeIDs[m.Type.ValueString()],
		Target:      m.Target.ValueString(),
		Name:        m.Name.ValueString(),
		Interval:    int(m.Interval.ValueInt64()),
		ContactIDs:  contactIDs,
		Port:        int(m.Port.ValueInt64()),
		SSL:         m.SSL.ValueBool(),
		Keywords:    keywords,
		KeywordMode: int(m.KeywordMode.ValueInt64()),
	}, diags
}

// toggleEnabled activates or deactivates the check to match want.
func (r *monitoringCheckResource) toggleEnabled(ctx context.Context, id int, want bool) error {
	if want {
		return r.client.MonitoringChecks.Activate(ctx, id)
	}
	return r.client.MonitoringChecks.Deactivate(ctx, id)
}

// applyRemote refreshes the computed/server-reported fields from a live check.
// The mutable inputs (target/interval/contacts/…) are not re-read: the index list
// does not report them, so the plan values stand.
func (r *monitoringCheckResource) applyRemote(m *monitoringCheckModel, check checks.Check) {
	m.ID = types.StringValue(check.ID)
	m.Name = types.StringValue(check.Name)
	m.Enabled = types.BoolValue(check.Status)
}

// listCheckIDs snapshots the current set of check ids for the create List-diff.
func (r *monitoringCheckResource) listCheckIDs(ctx context.Context) (map[string]struct{}, error) {
	list, err := r.client.MonitoringChecks.Index(ctx, nil)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(list.List))
	for _, c := range list.List {
		ids[c.ID] = struct{}{}
	}
	return ids, nil
}

// findNewCheck returns the check whose id is present now but not in before.
func (r *monitoringCheckResource) findNewCheck(ctx context.Context, before map[string]struct{}) (checks.Check, error) {
	list, err := r.client.MonitoringChecks.Index(ctx, nil)
	if err != nil {
		return checks.Check{}, err
	}
	for _, c := range list.List {
		if _, existed := before[c.ID]; !existed {
			return c, nil
		}
	}
	return checks.Check{}, fmt.Errorf("no new check appeared after create")
}

// findByID lists the checks and returns the one whose id matches.
func (r *monitoringCheckResource) findByID(ctx context.Context, id string) (checks.Check, bool, error) {
	list, err := r.client.MonitoringChecks.Index(ctx, nil)
	if err != nil {
		return checks.Check{}, false, err
	}
	for _, c := range list.List {
		if c.ID == id {
			return c, true, nil
		}
	}
	return checks.Check{}, false, nil
}
