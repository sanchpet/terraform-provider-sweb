package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

var (
	_ resource.Resource                = (*backupSettingsResource)(nil)
	_ resource.ResourceWithImportState = (*backupSettingsResource)(nil)
)

// NewBackupSettingsResource is the resource factory registered with the provider.
func NewBackupSettingsResource() resource.Resource { return &backupSettingsResource{} }

// backupSettingsResource manages a VPS's auto-backup schedule via the SDK Backup
// service (getSettings/saveSettings). The schedule always exists server-side, so
// Delete resets it to manual (auto-backups off) rather than removing anything.
type backupSettingsResource struct{ client *sweb.Client }

type backupSettingsModel struct {
	BillingID      types.String `tfsdk:"billing_id"`
	Mode           types.String `tfsdk:"mode"`
	Frequency      types.Int64  `tfsdk:"frequency"`
	Time           types.Int64  `tfsdk:"time"`
	NextDataBackup types.String `tfsdk:"next_data_backup"`
	ID             types.String `tfsdk:"id"`
}

func (r *backupSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vps_backup_settings"
}

func (r *backupSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a VPS's auto-backup schedule (getSettings/saveSettings on /vps/backup). " +
			"Destroying the resource resets the schedule to manual (auto-backups off).",
		Attributes: map[string]schema.Attribute{
			"billing_id": schema.StringAttribute{
				Required:      true,
				Description:   "VPS service id (login_vps_N) whose schedule is managed.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"mode": schema.StringAttribute{
				Required:    true,
				Description: "Backup mode: \"auto\" or \"manual\".",
			},
			"frequency": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Auto-backup frequency in days (auto mode).",
			},
			"time": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Auto-backup hour of day (auto mode).",
			},
			"next_data_backup": schema.StringAttribute{
				Computed:    true,
				Description: "Timestamp of the next scheduled backup.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (equals billing_id).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *backupSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// apply writes the desired schedule and reads it back into the model.
func (r *backupSettingsResource) apply(ctx context.Context, m *backupSettingsModel) error {
	if err := r.client.Backup.SaveSettings(ctx, m.BillingID.ValueString(),
		m.Mode.ValueString(), int(m.Frequency.ValueInt64()), int(m.Time.ValueInt64())); err != nil {
		return err
	}
	return r.refresh(ctx, m)
}

// refresh reads the current schedule into the model.
func (r *backupSettingsResource) refresh(ctx context.Context, m *backupSettingsModel) error {
	set, err := r.client.Backup.Settings(ctx, m.BillingID.ValueString())
	if err != nil {
		return err
	}
	m.ID = m.BillingID
	if set == nil {
		return nil
	}
	m.Mode = types.StringValue(set.Mode)
	m.Frequency = types.Int64Value(int64(set.Frequency))
	m.Time = types.Int64Value(int64(set.Time))
	m.NextDataBackup = types.StringValue(set.NextDataBackup)
	return nil
}

func (r *backupSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan backupSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Failed to set auto-backup schedule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *backupSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state backupSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.refresh(ctx, &state); err != nil {
		resp.Diagnostics.AddError("Failed to read auto-backup schedule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *backupSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan backupSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Failed to update auto-backup schedule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete resets the schedule to manual (auto-backups off) — the schedule is
// intrinsic to the VPS and cannot be removed.
func (r *backupSettingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state backupSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Backup.SaveSettings(ctx, state.BillingID.ValueString(), "manual", 0, 0); err != nil {
		resp.Diagnostics.AddError("Failed to reset auto-backup schedule", err.Error())
		return
	}
}

func (r *backupSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("billing_id"), req, resp)
}
