package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/sites"
)

var (
	_ resource.Resource                = (*siteResource)(nil)
	_ resource.ResourceWithImportState = (*siteResource)(nil)
)

// NewSiteResource is the resource factory registered with the provider.
func NewSiteResource() resource.Resource { return &siteResource{} }

// siteResource manages one shared-hosting website (add/edit/del + index on
// /sites). A site is identified by its content — its document root (`doc_root`),
// the home directory that is unique per site and the key edit/del expect. The
// site's binding (`domain`, `machine`) and its Redis-session flag are set at
// creation and force replacement; only the display `alias` updates in place.
type siteResource struct{ client *sweb.Client }

type siteModel struct {
	Alias              types.String `tfsdk:"alias"`
	DocRoot            types.String `tfsdk:"doc_root"`
	Domain             types.String `tfsdk:"domain"`
	Machine            types.String `tfsdk:"machine"`
	EnableRedisSession types.Bool   `tfsdk:"enable_redis_session"`
	DocRootFull        types.String `tfsdk:"doc_root_full"`
	SiteID             types.Int64  `tfsdk:"site_id"`
	ID                 types.String `tfsdk:"id"`
}

func (r *siteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_site"
}

func (r *siteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	forceNewStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages one shared-hosting website (add/edit/del on /sites). Identified by its document " +
			"root (`doc_root`); `domain`, `machine` and `enable_redis_session` are set at creation and force " +
			"replacement, while `alias` updates in place.",
		Attributes: map[string]schema.Attribute{
			"alias": schema.StringAttribute{
				Required:    true,
				Description: "The site name (display alias). Updated in place via edit.",
			},
			"doc_root": schema.StringAttribute{
				Required:      true,
				Description:   "The site's home directory — its stable identity and the edit/del key.",
				PlanModifiers: forceNewStr,
			},
			"domain": schema.StringAttribute{
				Required:      true,
				Description:   "The domain the site is created under (must already belong to the account).",
				PlanModifiers: forceNewStr,
			},
			"machine": schema.StringAttribute{
				Optional:      true,
				Description:   "Subdomain label to bind the site to (optional).",
				PlanModifiers: forceNewStr,
			},
			"enable_redis_session": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				Description:   "Whether to store PHP sessions in Redis. Set at creation only.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"doc_root_full": schema.StringAttribute{
				Computed:      true,
				Description:   "The absolute document root reported by the API.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"site_id": schema.Int64Attribute{
				Computed:    true,
				Description: "The API's numeric site id.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (equal to doc_root).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *siteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *siteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan siteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	docRoot := plan.DocRoot.ValueString()
	if err := r.client.Sites.Add(ctx, sites.AddOptions{
		Alias:              plan.Alias.ValueString(),
		DocRoot:            docRoot,
		Domain:             plan.Domain.ValueString(),
		Machine:            plan.Machine.ValueString(),
		EnableRedisSession: plan.EnableRedisSession.ValueBool(),
	}); err != nil {
		resp.Diagnostics.AddError("Failed to create site", err.Error())
		return
	}
	site, found, err := r.findSite(ctx, docRoot)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read site after create", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Site missing after create", fmt.Sprintf("site %q not found after add", docRoot))
		return
	}
	applySite(&plan, site)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *siteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state siteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	site, found, err := r.findSite(ctx, state.DocRoot.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read sites", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx) // removed outside Terraform
		return
	}
	applySite(&state, site)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *siteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state siteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	docRoot := plan.DocRoot.ValueString()
	// Only alias can change (doc_root/domain/machine/redis force replacement); a
	// zero docRootNew keeps the current directory.
	if !plan.Alias.Equal(state.Alias) {
		if err := r.client.Sites.Edit(ctx, docRoot, plan.Alias.ValueString(), ""); err != nil {
			resp.Diagnostics.AddError("Failed to update site", err.Error())
			return
		}
	}
	site, found, err := r.findSite(ctx, docRoot)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read site after update", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Site missing after update", fmt.Sprintf("site %q not found after edit", docRoot))
		return
	}
	applySite(&plan, site)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *siteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state siteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Sites.Del(ctx, state.DocRoot.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete site", err.Error())
		return
	}
}

// ImportState accepts the doc_root. Read then fills alias and the computed
// fields; domain/machine/enable_redis_session are not API-reported per site, so
// they stay as configured (or empty until set).
func (r *siteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("doc_root"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// findSite lists websites and returns the one whose doc_root matches.
func (r *siteResource) findSite(ctx context.Context, docRoot string) (sites.Site, bool, error) {
	list, err := r.client.Sites.List(ctx, nil)
	if err != nil {
		return sites.Site{}, false, err
	}
	for _, s := range list {
		if s.DocRoot == docRoot {
			return s, true, nil
		}
	}
	return sites.Site{}, false, nil
}

// applySite refreshes the mutable/computed fields from a live site. The binding
// inputs (domain/machine/enable_redis_session) are not per-site API-reported, so
// they are left as configured.
func applySite(m *siteModel, s sites.Site) {
	m.Alias = types.StringValue(s.Alias)
	m.DocRootFull = types.StringValue(s.DocRootFull)
	m.SiteID = types.Int64Value(int64(s.ID))
	m.ID = types.StringValue(s.DocRoot)
}
