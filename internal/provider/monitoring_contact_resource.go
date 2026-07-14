package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/monitoring/contacts"
)

var (
	_ resource.Resource                = (*monitoringContactResource)(nil)
	_ resource.ResourceWithImportState = (*monitoringContactResource)(nil)
)

// NewMonitoringContactResource is the resource factory registered with the provider.
func NewMonitoringContactResource() resource.Resource { return &monitoringContactResource{} }

// monitoringContactResource manages one monitoring contact — an email, phone, or
// Telegram destination notifications are sent to (addEmail/addPhone/addTelegram +
// index + editContact/deleteContact on /monitoring/contacts).
//
// Unlike the shared-hosting resources, create returns the new contact `id`
// directly (no List-diff needed): it is the Terraform identity and the import/
// delete key. `type` and `value` force replacement (the API's add methods are
// type-specific and `value` is not editable across types); `name` updates in
// place through the type's own edit method. `verified` is read-only — a Telegram
// contact must complete the bot verification flow out of band before it is true.
type monitoringContactResource struct{ client *sweb.Client }

type monitoringContactModel struct {
	Type     types.String `tfsdk:"type"`
	Value    types.String `tfsdk:"value"`
	Name     types.String `tfsdk:"name"`
	Verified types.Bool   `tfsdk:"verified"`
	ID       types.String `tfsdk:"id"`
}

func (r *monitoringContactResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitoring_contact"
}

func (r *monitoringContactResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	forceNewStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages one monitoring contact — an email, phone, or Telegram notification destination " +
			"(addEmail/addPhone/addTelegram + editContact/deleteContact on /monitoring/contacts). Create returns " +
			"the contact `id` directly, which is the resource identity. `type` and `value` force replacement; " +
			"`name` updates in place. `verified` is read-only (Telegram requires an out-of-band bot verification).",
		Attributes: map[string]schema.Attribute{
			"type": schema.StringAttribute{
				Required:      true,
				Description:   "Contact type: one of email, phone, telegram. Forces replacement.",
				PlanModifiers: forceNewStr,
				Validators: []validator.String{
					stringvalidator.OneOf(contacts.ContactEmail, contacts.ContactPhone, contacts.ContactTelegram),
				},
			},
			"value": schema.StringAttribute{
				Optional: true,
				Description: "The contact address — the email or phone number. Omitted for telegram (which " +
					"carries no value). Forces replacement.",
				PlanModifiers: forceNewStr,
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable label for the contact. Updated in place via the type's edit method.",
			},
			"verified": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the contact is confirmed and able to receive notifications (read-only).",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (the numeric contact id assigned by the API).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *monitoringContactResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *monitoringContactResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan monitoringContactModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, value := plan.Name.ValueString(), plan.Value.ValueString()

	var (
		id  int64
		err error
	)
	switch plan.Type.ValueString() {
	case contacts.ContactEmail:
		id, err = r.client.MonitoringContacts.AddEmail(ctx, value, name)
	case contacts.ContactPhone:
		id, err = r.client.MonitoringContacts.AddPhone(ctx, value, name)
	case contacts.ContactTelegram:
		id, err = r.client.MonitoringContacts.AddTelegram(ctx, name)
	default:
		resp.Diagnostics.AddError("Invalid contact type", "type must be one of email, phone, telegram")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to create monitoring contact", err.Error())
		return
	}

	// Read back to populate the computed fields (verified) from the live list.
	contact, found, err := r.findContact(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read monitoring contact after create", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Monitoring contact missing after create", fmt.Sprintf("contact %d not found after create", id))
		return
	}
	r.applyRemote(&plan, contact)
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *monitoringContactResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state monitoringContactModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.ParseInt(state.ID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid contact id in state", err.Error())
		return
	}
	contact, found, err := r.findContact(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read monitoring contacts", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx) // removed outside Terraform
		return
	}
	r.applyRemote(&state, contact)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *monitoringContactResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state monitoringContactModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// type and value force replacement, so only name reaches Update; the edit
	// methods for email/phone still take the (unchanged) value alongside it.
	id := state.ID.ValueString()
	name, value := plan.Name.ValueString(), state.Value.ValueString()

	var err error
	switch plan.Type.ValueString() {
	case contacts.ContactEmail:
		err = r.client.MonitoringContacts.EditEmail(ctx, id, value, name)
	case contacts.ContactPhone:
		err = r.client.MonitoringContacts.EditPhone(ctx, id, value, name)
	case contacts.ContactTelegram:
		err = r.client.MonitoringContacts.EditTelegram(ctx, id, name)
	default:
		resp.Diagnostics.AddError("Invalid contact type", "type must be one of email, phone, telegram")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to update monitoring contact", err.Error())
		return
	}

	nid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid contact id in state", err.Error())
		return
	}
	contact, found, err := r.findContact(ctx, nid)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read monitoring contact after update", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Monitoring contact missing after update", fmt.Sprintf("contact %s not found after update", id))
		return
	}
	r.applyRemote(&plan, contact)
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *monitoringContactResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state monitoringContactModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.MonitoringContacts.DeleteContact(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete monitoring contact", err.Error())
		return
	}
}

// ImportState accepts the numeric contact id. Read then fills type, value, name,
// and the computed verified flag from the live list.
func (r *monitoringContactResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// findContact lists the account's monitoring contacts and returns the one whose
// id matches (identity is the numeric id returned by the add methods).
func (r *monitoringContactResource) findContact(ctx context.Context, id int64) (contacts.Contact, bool, error) {
	list, err := r.client.MonitoringContacts.Index(ctx, nil)
	if err != nil {
		return contacts.Contact{}, false, err
	}
	for _, c := range list.List {
		if int64(c.ID) == id {
			return c, true, nil
		}
	}
	return contacts.Contact{}, false, nil
}

// applyRemote refreshes the fields from a live contact. type is a force-replace
// key but the list reports it, so import (which has no prior state) can recover it.
func (r *monitoringContactResource) applyRemote(m *monitoringContactModel, c contacts.Contact) {
	m.Type = types.StringValue(c.Type)
	m.Name = types.StringValue(c.Name)
	m.Verified = types.BoolValue(c.Verified)
	// The API reports no value for a Telegram contact; keep the config null rather
	// than clobbering it with an empty string (which would show spurious drift).
	if c.Type != contacts.ContactTelegram {
		m.Value = types.StringValue(c.Value)
	}
}
