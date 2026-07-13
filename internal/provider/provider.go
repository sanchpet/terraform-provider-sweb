// Package provider implements the Terraform provider for SpaceWeb (sweb.ru),
// built on the Terraform Plugin Framework over github.com/sanchpet/sweb-go-sdk.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

// New returns the provider factory used by providerserver.Serve.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &swebProvider{version: version} }
}

type swebProvider struct{ version string }

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
	Login    types.String `tfsdk:"login"`
	Password types.String `tfsdk:"password"`
}

func (p *swebProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "sweb"
	resp.Version = p.version
}

func (p *swebProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage SpaceWeb (sweb.ru) resources.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "API root override. Falls back to $SWEB_ENDPOINT, then the production API.",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "API token. Falls back to $SWEB_TOKEN. One-off (no refresh).",
			},
			"login": schema.StringAttribute{
				Optional:    true,
				Description: "Login for transparent token refresh. Falls back to $SWEB_LOGIN.",
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Password for transparent token refresh. Falls back to $SWEB_PASSWORD.",
			},
		},
	}
}

func (p *swebProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	token := firstNonEmpty(cfg.Token.ValueString(), os.Getenv("SWEB_TOKEN"))
	login := firstNonEmpty(cfg.Login.ValueString(), os.Getenv("SWEB_LOGIN"))
	password := firstNonEmpty(cfg.Password.ValueString(), os.Getenv("SWEB_PASSWORD"))

	if token == "" && (login == "" || password == "") {
		resp.Diagnostics.AddError(
			"Missing SpaceWeb credentials",
			"Set `token` (or $SWEB_TOKEN), or `login`+`password` (or $SWEB_LOGIN/$SWEB_PASSWORD).",
		)
		return
	}

	opts := []sweb.Option{}
	if endpoint := firstNonEmpty(cfg.Endpoint.ValueString(), os.Getenv("SWEB_ENDPOINT")); endpoint != "" {
		opts = append(opts, sweb.WithBaseURL(endpoint))
	}
	if token != "" {
		opts = append(opts, sweb.WithToken(token))
	}
	if login != "" && password != "" {
		opts = append(opts, sweb.WithCredentials(login, password))
	}

	client := sweb.New(opts...)
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *swebProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewVPSResource, NewLocalNetworkResource, NewPTRRecordResource, NewBackupSettingsResource,
		NewSubdomainResource, NewDomainRedirectResource, NewDNSRecordResource, NewDNSSRVRecordResource,
		NewMailboxResource,
	}
}

func (p *swebProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewPlanDataSource, NewDomainDataSource}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
