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
	_ resource.Resource                = (*domainRedirectResource)(nil)
	_ resource.ResourceWithImportState = (*domainRedirectResource)(nil)
)

// NewDomainRedirectResource is the resource factory registered with the provider.
func NewDomainRedirectResource() resource.Resource { return &domainRedirectResource{} }

// domainRedirectResource manages a domain's redirect URL (setRedirectVh/getRedirectVh
// on /domains). A domain always has a redirect slot (empty = no redirect), so — like
// sweb_ptr_record — Delete clears it rather than removing anything. The domain must
// already belong to the account.
type domainRedirectResource struct{ client *sweb.Client }

type domainRedirectModel struct {
	Domain types.String `tfsdk:"domain"`
	URL    types.String `tfsdk:"url"`
	ID     types.String `tfsdk:"id"`
}

func (r *domainRedirectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_redirect"
}

func (r *domainRedirectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a domain's redirect URL (setRedirectVh/getRedirectVh on /domains). The domain " +
			"must already belong to the account. Destroying the resource clears the redirect.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				Required:      true,
				Description:   "The domain whose redirect is managed. Changing it targets a different domain.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"url": schema.StringAttribute{
				Required:    true,
				Description: "The URL to redirect the domain to.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (equals domain).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *domainRedirectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *domainRedirectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainRedirectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Domains.SetRedirect(ctx, plan.Domain.ValueString(), plan.URL.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to set redirect", err.Error())
		return
	}
	plan.ID = plan.Domain
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainRedirectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainRedirectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	url, err := r.client.Domains.Redirect(ctx, state.Domain.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read redirect", err.Error())
		return
	}
	state.ID = state.Domain
	state.URL = types.StringValue(url)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *domainRedirectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan domainRedirectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Domains.SetRedirect(ctx, plan.Domain.ValueString(), plan.URL.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to update redirect", err.Error())
		return
	}
	plan.ID = plan.Domain
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete clears the redirect (setRedirectVh with an empty URL) — the slot is
// intrinsic to the domain and cannot be removed.
func (r *domainRedirectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainRedirectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Domains.SetRedirect(ctx, state.Domain.ValueString(), ""); err != nil {
		resp.Diagnostics.AddError("Failed to clear redirect", err.Error())
		return
	}
}

func (r *domainRedirectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("domain"), req, resp)
}
