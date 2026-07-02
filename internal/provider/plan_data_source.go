package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

// NewPlanDataSource is the factory registered with the provider.
func NewPlanDataSource() datasource.DataSource { return &planDataSource{} }

// planDataSource resolves a SpaceWeb configurator spec (cpu/ram/disk[/category])
// to a plan id, so HCL can describe a node by its readable resources instead of a
// magic plan number. It calls the same resolver (getConstructorPlanId) the sweb_vps
// resource uses, so `plan = data.sweb_plan.x.id` provisions exactly what the spec
// describes — and an already-imported plan-mode node keeps a clean plan (the data
// source just re-derives the id it was created with, without switching modes).
type planDataSource struct {
	client *sweb.Client
}

type planDataSourceModel struct {
	CPU      types.Int64 `tfsdk:"cpu"`
	RAM      types.Int64 `tfsdk:"ram"`
	Disk     types.Int64 `tfsdk:"disk"`
	Category types.Int64 `tfsdk:"category"`
	ID       types.Int64 `tfsdk:"id"`
}

func (d *planDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plan"
}

func (d *planDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Resolves a configurator spec (CPU/RAM/disk[/category]) to a SpaceWeb plan id, so a " +
			"sweb_vps can be described by readable resources instead of a magic plan number. Feed the " +
			"result into sweb_vps.plan. Note: the id is resolved dynamically each plan, so a catalog remap " +
			"on SpaceWeb's side could change it — pin a literal plan if you need a frozen id.",
		Attributes: map[string]schema.Attribute{
			"cpu": schema.Int64Attribute{
				Required:    true,
				Description: "CPU cores.",
			},
			"ram": schema.Int64Attribute{
				Required:    true,
				Description: "RAM in GB.",
			},
			"disk": schema.Int64Attribute{
				Required:    true,
				Description: "Disk in GB.",
			},
			"category": schema.Int64Attribute{
				Optional:    true,
				Description: "Catalog category id (1=NVMe, 2=HDD, 3=Turbo). Defaults to 1 (NVMe).",
			},
			"id": schema.Int64Attribute{
				Computed:    true,
				Description: "The resolved plan id — feed into sweb_vps.plan.",
			},
		},
	}
}

func (d *planDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*sweb.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *sweb.Client, got %T", req.ProviderData))
		return
	}
	d.client = client
}

func (d *planDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data planDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	category := int(data.Category.ValueInt64())
	if data.Category.IsNull() {
		category = 1 // NVMe
	}

	id, err := d.client.VPS.GetConstructorPlanID(ctx,
		int(data.CPU.ValueInt64()), int(data.RAM.ValueInt64()), int(data.Disk.ValueInt64()), category)
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve plan", err.Error())
		return
	}

	data.ID = types.Int64Value(int64(id))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
