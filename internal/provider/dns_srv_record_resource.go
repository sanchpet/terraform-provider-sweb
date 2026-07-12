package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

var (
	_ resource.Resource                = (*dnsSRVRecordResource)(nil)
	_ resource.ResourceWithImportState = (*dnsSRVRecordResource)(nil)
)

// NewDNSSRVRecordResource is the resource factory registered with the provider.
func NewDNSSRVRecordResource() resource.Resource { return &dnsSRVRecordResource{} }

// dnsSRVRecordResource manages a single SRV record (editSrv + info on
// /domains/dns). SRV has its own shape — service/protocol/target/port/weight —
// so it is a separate resource from sweb_dns_record. It uses the same
// content-addressed identity: the record is matched on its content and the wire
// index re-derived before each read/delete; every attribute forces replacement.
type dnsSRVRecordResource struct{ client *sweb.Client }

type dnsSRVRecordModel struct {
	Domain   types.String `tfsdk:"domain"`
	Service  types.String `tfsdk:"service"`
	Protocol types.String `tfsdk:"protocol"`
	Name     types.String `tfsdk:"name"`
	Target   types.String `tfsdk:"target"`
	Port     types.Int64  `tfsdk:"port"`
	Priority types.Int64  `tfsdk:"priority"`
	Weight   types.Int64  `tfsdk:"weight"`
	TTL      types.Int64  `tfsdk:"ttl"`
	ID       types.String `tfsdk:"id"`
}

func (r *dnsSRVRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_srv_record"
}

func (r *dnsSRVRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	forceNewStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	forceNewInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages a single SRV record of a zone on the account (editSrv on /domains/dns). The " +
			"domain must already belong to the account. Like sweb_dns_record it is identified by content, " +
			"not the API's shifting index, and every attribute forces replacement.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				Required:      true,
				Description:   "The zone (domain) the record belongs to.",
				PlanModifiers: forceNewStr,
			},
			"service": schema.StringAttribute{
				Required:      true,
				Description:   "Service name without the leading underscore (e.g. \"sip\", \"autodiscover\").",
				PlanModifiers: forceNewStr,
			},
			"protocol": schema.StringAttribute{
				Required:      true,
				Description:   "Protocol: tcp or udp.",
				Validators:    []validator.String{stringvalidator.OneOf("tcp", "udp")},
				PlanModifiers: forceNewStr,
			},
			"name": schema.StringAttribute{
				Optional:      true,
				Description:   "Host label the service sits under. Empty or \"@\" for the zone apex.",
				PlanModifiers: forceNewStr,
			},
			"target": schema.StringAttribute{
				Required:      true,
				Description:   "Target host that provides the service.",
				PlanModifiers: forceNewStr,
			},
			"port": schema.Int64Attribute{
				Required:      true,
				Description:   "Target port.",
				PlanModifiers: forceNewInt,
			},
			"priority": schema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Default:       int64default.StaticInt64(0),
				Description:   "Priority (lower is preferred). Defaults to 0.",
				PlanModifiers: forceNewInt,
			},
			"weight": schema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Default:       int64default.StaticInt64(0),
				Description:   "Relative weight among equal-priority targets. Defaults to 0.",
				PlanModifiers: forceNewInt,
			},
			"ttl": schema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Record TTL in seconds. Assigned by the API when omitted.",
				PlanModifiers: forceNewInt,
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (\"<domain>/<service>/<protocol>/<name>/<target>/<port>\").",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *dnsSRVRecordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *dnsSRVRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsSRVRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DNS.EditSRV(ctx, plan.Domain.ValueString(), sweb.DNSActionAdd, srvRecord(plan, 0)); err != nil {
		resp.Diagnostics.AddError("Failed to create SRV record", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsSRVRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsSRVRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rec, found, err := r.find(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read DNS zone", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	state.Priority = types.Int64Value(int64(rec.Priority))
	state.Weight = types.Int64Value(int64(rec.Weight))
	state.TTL = types.Int64Value(int64(rec.TTL))
	state.ID = types.StringValue(srvRecordID(state))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update never runs — every attribute forces replacement.
func (r *dnsSRVRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsSRVRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(srvRecordID(plan))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsSRVRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsSRVRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rec, found, err := r.find(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read DNS zone", err.Error())
		return
	}
	if !found {
		return
	}
	if err := r.client.DNS.EditSRV(ctx, state.Domain.ValueString(), sweb.DNSActionRemove, sweb.SRVRecord{Index: int(rec.Index)}); err != nil {
		resp.Diagnostics.AddError("Failed to delete SRV record", err.Error())
		return
	}
}

// ImportState accepts "<domain>/<service>/<protocol>/<name>/<target>/<port>".
func (r *dnsSRVRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 6 {
		resp.Diagnostics.AddError("Invalid import id",
			"expected \"<domain>/<service>/<protocol>/<name>/<target>/<port>\", got "+req.ID)
		return
	}
	port, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", "port is not a number: "+parts[5])
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("protocol"), parts[2])...)
	if parts[3] != "" { // leave an omitted apex label null, matching config
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[3])...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target"), parts[4])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("port"), port)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// refresh re-reads the just-written record and fills the API-assigned fields
// (priority/weight/ttl) into the model.
func (r *dnsSRVRecordResource) refresh(ctx context.Context, m *dnsSRVRecordModel, diags *diag.Diagnostics) {
	rec, found, err := r.find(ctx, *m)
	if err != nil {
		diags.AddError("Failed to read back SRV record", err.Error())
		return
	}
	if !found {
		diags.AddError("SRV record not found after create", "the record did not appear in the zone")
		return
	}
	m.Priority = types.Int64Value(int64(rec.Priority))
	m.Weight = types.Int64Value(int64(rec.Weight))
	m.TTL = types.Int64Value(int64(rec.TTL))
	m.ID = types.StringValue(srvRecordID(*m))
}

// find locates the SRV record matching a resource's content (service, protocol,
// host, target, port) and returns it with its current wire index.
func (r *dnsSRVRecordResource) find(ctx context.Context, m dnsSRVRecordModel) (sweb.DNSRecord, bool, error) {
	recs, err := r.client.DNS.Records(ctx, m.Domain.ValueString())
	if err != nil {
		return sweb.DNSRecord{}, false, err
	}
	host := apexNorm(m.Name.ValueString())
	for _, rec := range recs {
		if !strings.EqualFold(rec.Type, "SRV") {
			continue
		}
		if rec.Service != m.Service.ValueString() || rec.Protocol != m.Protocol.ValueString() {
			continue
		}
		if apexNorm(rec.Name) != host {
			continue
		}
		if strings.TrimSuffix(rec.Target, ".") != strings.TrimSuffix(m.Target.ValueString(), ".") {
			continue
		}
		if int64(rec.Port) != m.Port.ValueInt64() {
			continue
		}
		return rec, true, nil
	}
	return sweb.DNSRecord{}, false, nil
}

func srvRecord(m dnsSRVRecordModel, index int) sweb.SRVRecord {
	return sweb.SRVRecord{
		Index:     index,
		Priority:  int(m.Priority.ValueInt64()),
		TTL:       int(m.TTL.ValueInt64()),
		Weight:    int(m.Weight.ValueInt64()),
		Target:    m.Target.ValueString(),
		Service:   m.Service.ValueString(),
		Protocol:  m.Protocol.ValueString(),
		Port:      int(m.Port.ValueInt64()),
		SubDomain: apexNorm(m.Name.ValueString()),
	}
}

func srvRecordID(m dnsSRVRecordModel) string {
	return strings.Join([]string{
		m.Domain.ValueString(), m.Service.ValueString(), m.Protocol.ValueString(),
		m.Name.ValueString(), m.Target.ValueString(), strconv.FormatInt(m.Port.ValueInt64(), 10),
	}, "/")
}
