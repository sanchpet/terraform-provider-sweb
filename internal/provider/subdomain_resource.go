package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/domains"
)

var (
	_ resource.Resource                = (*subdomainResource)(nil)
	_ resource.ResourceWithImportState = (*subdomainResource)(nil)
)

// NewSubdomainResource is the resource factory registered with the provider.
func NewSubdomainResource() resource.Resource { return &subdomainResource{} }

// subdomainResource manages a subdomain of a domain already on the account
// (createSubdomain/removeSubdomain/getSubdomains on /domains). The lifecycle maps
// cleanly onto Terraform: create adds the subdomain, destroy removes it. Every
// attribute forces replacement, so there is no in-place update.
type subdomainResource struct{ client *sweb.Client }

type subdomainModel struct {
	Domain  types.String `tfsdk:"domain"`
	Machine types.String `tfsdk:"machine"`
	Dir     types.String `tfsdk:"dir"`
	ID      types.String `tfsdk:"id"`
}

func (r *subdomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subdomain"
}

func (r *subdomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a subdomain of a domain on the account (createSubdomain/removeSubdomain on " +
			"/domains). The parent domain must already belong to the account. Destroying the resource " +
			"removes the subdomain.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				Required:      true,
				Description:   "The parent domain (must already be on the account).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"machine": schema.StringAttribute{
				Required:      true,
				Description:   "The subdomain label (e.g. \"shop\" for shop.example.com, or \"*\" for a wildcard).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"dir": schema.StringAttribute{
				Optional: true,
				Description: "Site directory for the subdomain. Set at creation only; its drift is not " +
					"tracked (the API reports an absolute docroot, not the value sent here).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (\"<domain>/<machine>\").",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *subdomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *subdomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan subdomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, machine := plan.Domain.ValueString(), plan.Machine.ValueString()
	if err := r.client.Domains.CreateSubdomain(ctx, domain, machine, plan.Dir.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to create subdomain", err.Error())
		return
	}
	plan.ID = types.StringValue(subdomainID(domain, machine))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *subdomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state subdomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, machine := state.Domain.ValueString(), state.Machine.ValueString()
	subs, err := r.client.Domains.Subdomains(ctx, domain)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read subdomains", err.Error())
		return
	}
	if !subdomainPresent(subs, domain, machine) {
		resp.State.RemoveResource(ctx) // removed outside Terraform
		return
	}
	state.ID = types.StringValue(subdomainID(domain, machine))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update never runs a real change — every attribute forces replacement — but the
// framework requires the method. Persist the plan defensively.
func (r *subdomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan subdomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(subdomainID(plan.Domain.ValueString(), plan.Machine.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *subdomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state subdomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Domains.RemoveSubdomain(ctx, state.Domain.ValueString(), state.Machine.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to remove subdomain", err.Error())
		return
	}
}

// ImportState accepts "<domain>/<machine>".
func (r *subdomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	domain, machine, ok := strings.Cut(req.ID, "/")
	if !ok || domain == "" || machine == "" {
		resp.Diagnostics.AddError("Invalid import id", "expected \"<domain>/<machine>\", got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), domain)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("machine"), machine)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), subdomainID(domain, machine))...)
}

func subdomainID(domain, machine string) string { return domain + "/" + machine }

// subdomainPresent reports whether machine.domain appears in the getSubdomains
// list. The API returns each subdomain as its full name (encoded and readable),
// so match against "<machine>.<domain>".
func subdomainPresent(subs []domains.SubdomainRef, domain, machine string) bool {
	fqdn := machine + "." + domain
	for _, s := range subs {
		if s.Value == fqdn || s.Name == fqdn {
			return true
		}
	}
	return false
}
