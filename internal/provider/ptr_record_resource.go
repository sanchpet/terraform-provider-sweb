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
	_ resource.Resource                = (*ptrRecordResource)(nil)
	_ resource.ResourceWithImportState = (*ptrRecordResource)(nil)
)

// NewPTRRecordResource is the resource factory registered with the provider.
func NewPTRRecordResource() resource.Resource { return &ptrRecordResource{} }

// ptrRecordResource manages the reverse-DNS (PTR) record of a public IP via the
// SDK IP service (getPtr/editPtr). The PTR always exists server-side, so Delete
// resets it to the provider default rather than removing anything.
type ptrRecordResource struct{ client *sweb.Client }

type ptrRecordModel struct {
	IP  types.String `tfsdk:"ip"`
	PTR types.String `tfsdk:"ptr"`
	ID  types.String `tfsdk:"id"`
}

func (r *ptrRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ptr_record"
}

func (r *ptrRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the reverse-DNS (PTR) record of a public IP (getPtr/editPtr on " +
			"/vps/ip). The IP must already belong to the account. Destroying the resource resets " +
			"the PTR to the provider default.",
		Attributes: map[string]schema.Attribute{
			"ip": schema.StringAttribute{
				Required:      true,
				Description:   "The public IP whose PTR record is managed. Changing it targets a different IP.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ptr": schema.StringAttribute{
				Required:    true,
				Description: "The reverse-DNS name to set for the IP.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (equals ip).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ptrRecordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ptrRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ptrRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.IP.EditPtr(ctx, plan.IP.ValueString(), plan.PTR.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to set PTR record", err.Error())
		return
	}
	plan.ID = plan.IP
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ptrRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ptrRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ptr, err := r.client.IP.GetPtr(ctx, state.IP.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read PTR record", err.Error())
		return
	}
	state.ID = state.IP
	state.PTR = types.StringValue(ptr)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ptrRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ptrRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.IP.EditPtr(ctx, plan.IP.ValueString(), plan.PTR.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to update PTR record", err.Error())
		return
	}
	plan.ID = plan.IP
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete resets the PTR to the provider default (an empty ptr) — the record
// itself is intrinsic to the IP and cannot be removed.
func (r *ptrRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ptrRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.IP.EditPtr(ctx, state.IP.ValueString(), ""); err != nil {
		resp.Diagnostics.AddError("Failed to reset PTR record", err.Error())
		return
	}
}

func (r *ptrRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("ip"), req, resp)
}
