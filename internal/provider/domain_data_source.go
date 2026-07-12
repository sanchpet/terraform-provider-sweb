package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

// NewDomainDataSource is the factory registered with the provider.
func NewDomainDataSource() datasource.DataSource { return &domainDataSource{} }

// domainDataSource reads a domain already on the account (getDomainInfo on
// /domains), exposing its registration and management attributes for reference —
// e.g. to feed a redirect or subdomain resource, or to surface an expiry date.
type domainDataSource struct {
	client *sweb.Client
}

type domainDataSourceModel struct {
	Domain        types.String `tfsdk:"domain"`
	Registrar     types.String `tfsdk:"registrar"`
	Expired       types.String `tfsdk:"expired"`
	IsOur         types.Bool   `tfsdk:"is_our"`
	CanProlong    types.Bool   `tfsdk:"can_prolong"`
	Autoprolong   types.String `tfsdk:"autoprolong"`
	RegPrice      types.Int64  `tfsdk:"reg_price"`
	TransferPrice types.Int64  `tfsdk:"transfer_price"`
	DocRoot       types.String `tfsdk:"docroot"`
	SiteAlias     types.String `tfsdk:"site_alias"`
	RedirectURL   types.String `tfsdk:"redirect_url"`
	ID            types.String `tfsdk:"id"`
}

func (d *domainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads a domain already on the account (getDomainInfo on /domains).",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				Required:    true,
				Description: "The domain to look up (must be on the account).",
			},
			"registrar":      schema.StringAttribute{Computed: true, Description: "Registrar, empty if unknown."},
			"expired":        schema.StringAttribute{Computed: true, Description: "Registration expiry date, empty if unknown."},
			"is_our":         schema.BoolAttribute{Computed: true, Description: "Whether the domain is under SpaceWeb management."},
			"can_prolong":    schema.BoolAttribute{Computed: true, Description: "Whether the domain can be prolonged now."},
			"autoprolong":    schema.StringAttribute{Computed: true, Description: "Auto-prolongation mode (no/manual/bonus_money)."},
			"reg_price":      schema.Int64Attribute{Computed: true, Description: "Registration price."},
			"transfer_price": schema.Int64Attribute{Computed: true, Description: "Transfer price, -1 when transfer is not offered."},
			"docroot":        schema.StringAttribute{Computed: true, Description: "Home directory."},
			"site_alias":     schema.StringAttribute{Computed: true, Description: "Site name in the control panel."},
			"redirect_url":   schema.StringAttribute{Computed: true, Description: "Configured redirect URL, empty if none."},
			"id":             schema.StringAttribute{Computed: true, Description: "Data source id (equals domain)."},
		},
	}
}

func (d *domainDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*sweb.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *sweb.Client, got %T", req.ProviderData))
		return
	}
	d.client = client
}

func (d *domainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data domainDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := data.Domain.ValueString()
	info, err := d.client.Domains.Info(ctx, domain)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read domain", err.Error())
		return
	}
	data.Registrar = types.StringValue(info.Registrar)
	data.Expired = types.StringValue(info.Expired)
	data.IsOur = types.BoolValue(info.IsOur == 1)
	data.CanProlong = types.BoolValue(info.CanProlong == 1)
	data.Autoprolong = types.StringValue(info.Autoprolong)
	data.RegPrice = types.Int64Value(int64(info.RegPrice))
	data.TransferPrice = types.Int64Value(int64(info.TransferPrice))
	data.DocRoot = types.StringValue(info.DocRoot)
	data.SiteAlias = types.StringValue(info.SiteAlias)
	data.RedirectURL = types.StringValue(info.RedirectURL)
	data.ID = types.StringValue(domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
