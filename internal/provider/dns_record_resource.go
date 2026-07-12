package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

var (
	_ resource.Resource                = (*dnsRecordResource)(nil)
	_ resource.ResourceWithImportState = (*dnsRecordResource)(nil)
)

// NewDNSRecordResource is the resource factory registered with the provider.
func NewDNSRecordResource() resource.Resource { return &dnsRecordResource{} }

// dnsRecordResource manages a single DNS record of a zone on the account
// (editMain/editMx/editTxt/editNS + info on /domains/dns), for the record types
// A, AAAA, CNAME, MX, TXT, NS.
//
// The SpaceWeb API addresses records by a per-type index that shifts as the zone
// changes, so it is never a stable Terraform id. Instead the record is identified
// by its content (type + host + value); the wire index is re-derived by matching
// on that content immediately before every read/delete. Every attribute forces
// replacement — a value change is a delete+create — which sidesteps in-place edit
// against the shifting index.
type dnsRecordResource struct{ client *sweb.Client }

type dnsRecordModel struct {
	Domain   types.String `tfsdk:"domain"`
	Type     types.String `tfsdk:"type"`
	Name     types.String `tfsdk:"name"`
	Value    types.String `tfsdk:"value"`
	Priority types.Int64  `tfsdk:"priority"`
	ID       types.String `tfsdk:"id"`
}

func (r *dnsRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func (r *dnsRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	forceNewStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages a single DNS record of a zone on the account (A/AAAA/CNAME/MX/TXT/NS via " +
			"editMain/editMx/editTxt/editNS on /domains/dns). The domain must already belong to the " +
			"account. The record is identified by its content, not the API's shifting per-type index, so " +
			"every attribute forces replacement — changing a value is a delete+create.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				Required:      true,
				Description:   "The zone (domain) the record belongs to.",
				PlanModifiers: forceNewStr,
			},
			"type": schema.StringAttribute{
				Required:      true,
				Description:   "Record type: A, AAAA, CNAME, MX, TXT, or NS.",
				Validators:    []validator.String{stringvalidator.OneOf("A", "AAAA", "CNAME", "MX", "TXT", "NS")},
				PlanModifiers: forceNewStr,
			},
			"name": schema.StringAttribute{
				Optional:      true,
				Description:   "Host label (e.g. \"www\"). Empty or \"@\" for the zone apex.",
				PlanModifiers: forceNewStr,
			},
			"value": schema.StringAttribute{
				Required:      true,
				Description:   "Record value: an IP for A/AAAA, the target host for CNAME/NS/MX, or the text for TXT.",
				PlanModifiers: forceNewStr,
			},
			"priority": schema.Int64Attribute{
				Optional:      true,
				Description:   "Priority — required for MX, ignored otherwise.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (\"<domain>/<type>/<name>/<value>\").",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *dnsRecordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *dnsRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if isMX(plan) && plan.Priority.IsNull() {
		resp.Diagnostics.AddError("Missing MX priority", "an MX record requires `priority`.")
		return
	}
	if err := r.edit(ctx, sweb.DNSActionAdd, plan, 0); err != nil {
		resp.Diagnostics.AddError("Failed to create DNS record", err.Error())
		return
	}
	plan.ID = types.StringValue(dnsRecordID(plan))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	recs, err := r.client.DNS.Records(ctx, state.Domain.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read DNS zone", err.Error())
		return
	}
	rec, found := findDNSRecord(recs, state)
	if !found {
		resp.State.RemoveResource(ctx) // changed or removed outside Terraform
		return
	}
	// Refresh MX priority from the live record so import populates it and a
	// priority change is detected (it forces replacement).
	if isMX(state) {
		state.Priority = types.Int64Value(int64(rec.Priority))
	}
	state.ID = types.StringValue(dnsRecordID(state))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update never runs — every attribute forces replacement — but the framework
// requires the method.
func (r *dnsRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(dnsRecordID(plan))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	recs, err := r.client.DNS.Records(ctx, state.Domain.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read DNS zone", err.Error())
		return
	}
	rec, found := findDNSRecord(recs, state)
	if !found {
		return // already gone
	}
	if err := r.edit(ctx, sweb.DNSActionRemove, state, int(rec.Index)); err != nil {
		resp.Diagnostics.AddError("Failed to delete DNS record", err.Error())
		return
	}
}

// ImportState accepts "<domain>/<type>/<name>/<value>" (name may be empty for the
// apex; value keeps any embedded slashes). MX priority is not encoded — set it in
// config; Read then reconciles it.
func (r *dnsRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 4)
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[3] == "" {
		resp.Diagnostics.AddError("Invalid import id", "expected \"<domain>/<type>/<name>/<value>\", got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), strings.ToUpper(parts[1]))...)
	if parts[2] != "" { // leave an omitted apex label null, matching config
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[2])...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("value"), parts[3])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// edit dispatches an add/remove to the SDK method for the record's type. For a
// remove, index is the record's current wire index (re-derived by the caller).
func (r *dnsRecordResource) edit(ctx context.Context, action sweb.DNSAction, m dnsRecordModel, index int) error {
	domain := m.Domain.ValueString()
	name := apexNorm(m.Name.ValueString())
	value := m.Value.ValueString()
	switch recordType(m) {
	case "A", "AAAA", "CNAME":
		return r.client.DNS.EditMain(ctx, domain, action, sweb.MainRecord{Index: index, Name: name, Type: recordType(m), Value: value})
	case "NS":
		return r.client.DNS.EditNS(ctx, domain, action, index, name, value)
	case "TXT":
		return r.client.DNS.EditTXT(ctx, domain, action, index, name, value)
	case "MX":
		return r.client.DNS.EditMX(ctx, domain, action, sweb.MXRecord{Index: index, Priority: int(m.Priority.ValueInt64()), Value: value, SubDomain: name})
	}
	return fmt.Errorf("unsupported record type %q", m.Type.ValueString())
}

func recordType(m dnsRecordModel) string { return strings.ToUpper(m.Type.ValueString()) }
func isMX(m dnsRecordModel) bool         { return recordType(m) == "MX" }

// dnsRecordID is the stable, content-derived id.
func dnsRecordID(m dnsRecordModel) string {
	return strings.Join([]string{m.Domain.ValueString(), recordType(m), m.Name.ValueString(), m.Value.ValueString()}, "/")
}

// findDNSRecord locates the zone record matching a resource's content (type,
// host, value), returning it and its current wire index. The host lives in the
// Domain field for TXT and in Name for the other types; CNAME/NS/MX targets are
// compared trailing-dot-insensitively.
func findDNSRecord(recs []sweb.DNSRecord, m dnsRecordModel) (sweb.DNSRecord, bool) {
	want := recordType(m)
	host := apexNorm(m.Name.ValueString())
	value := m.Value.ValueString()
	for _, rec := range recs {
		if !strings.EqualFold(rec.Type, want) {
			continue
		}
		if dnsHost(rec) != host {
			continue
		}
		if dnsValueMatches(want, rec.Value, value) {
			return rec, true
		}
	}
	return sweb.DNSRecord{}, false
}

// dnsHost returns a record's host label, normalized so the apex is "".
func dnsHost(rec sweb.DNSRecord) string {
	if strings.EqualFold(rec.Type, "TXT") {
		return apexNorm(rec.Domain)
	}
	return apexNorm(rec.Name)
}

// dnsValueMatches compares record values, ignoring a trailing dot for the
// host-valued types (CNAME/NS/MX) so "host.example.com" and "host.example.com."
// are the same.
func dnsValueMatches(recType, got, want string) bool {
	switch recType {
	case "CNAME", "NS", "MX":
		return strings.TrimSuffix(got, ".") == strings.TrimSuffix(want, ".")
	default:
		return got == want
	}
}

func apexNorm(s string) string {
	if s == "@" {
		return ""
	}
	return s
}
