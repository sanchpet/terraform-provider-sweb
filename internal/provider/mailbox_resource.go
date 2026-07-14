package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/vh/mail"
)

var (
	_ resource.Resource                = (*mailboxResource)(nil)
	_ resource.ResourceWithImportState = (*mailboxResource)(nil)
)

// antispamLevels maps the human antispam name to the API's numeric filter level
// (Mailbox.Antispam: 5 hard, 8 medium, 10 soft, 0 off).
var antispamLevels = map[string]int{"hard": 5, "medium": 8, "soft": 10, "off": 0}

// NewMailboxResource is the resource factory registered with the provider.
func NewMailboxResource() resource.Resource { return &mailboxResource{} }

// mailboxResource manages a single shared-hosting mailbox on a mail domain
// (createMbox/dropMbox/getMailboxesList + the per-field setters on /vh/mail). The
// mail domain must already belong to the account.
//
// A mailbox has no stable server id — like a DNS record, its identity is its
// content: the mail `domain` plus the local-part `name` (the part before @). Both
// force replacement; the wire list is matched on `name` immediately before every
// read/delete. The mutable fields (password, antispam, spf, comment) each update
// in place through their own setter.
type mailboxResource struct{ client *sweb.Client }

type mailboxModel struct {
	Domain            types.String `tfsdk:"domain"`
	Name              types.String `tfsdk:"name"`
	Password          types.String `tfsdk:"password"`
	PasswordWOVersion types.Int64  `tfsdk:"password_wo_version"`
	Quota             types.Int64  `tfsdk:"quota"`
	Antispam          types.String `tfsdk:"antispam"`
	SPF               types.Bool   `tfsdk:"spf"`
	Comment           types.String `tfsdk:"comment"`
	ID                types.String `tfsdk:"id"`
}

func (r *mailboxResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mailbox"
}

func (r *mailboxResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	forceNewStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages a single shared-hosting mailbox on a mail domain (createMbox/dropMbox on " +
			"/vh/mail). The mail domain must already belong to the account. The mailbox is identified by " +
			"its content — the mail `domain` plus the local-part `name` — not a server id, so `domain` and " +
			"`name` force replacement; password, antispam, spf and comment update in place.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				Required:      true,
				Description:   "The mail domain the mailbox belongs to (must already be on the account).",
				PlanModifiers: forceNewStr,
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "The mailbox local part — the label before @ (e.g. \"info\" for info@example.com).",
				PlanModifiers: forceNewStr,
			},
			"password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
				Description: "The mailbox password. Write-only — never stored in state, so importing a mailbox " +
					"never needs it. Required when creating a mailbox; rotate an existing one by changing it " +
					"together with password_wo_version.",
			},
			"password_wo_version": schema.Int64Attribute{
				Optional: true,
				Description: "Rotation trigger for the write-only password. Bump it (with a new password) to apply a " +
					"password change; write-only values can't be diffed from state, so this nonce drives the update.",
			},
			"quota": schema.Int64Attribute{
				Computed: true,
				Description: "Mailbox size quota in MB, as assigned and reported by the API. Read-only: the " +
					"API exposes no create- or update-time quota control on a shared-hosting mailbox.",
			},
			"antispam": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("off"),
				Description: "Antispam filter level: one of hard, medium, soft, off. Updated via updateAntispamState.",
				Validators:  []validator.String{stringvalidator.OneOf("hard", "medium", "soft", "off")},
			},
			"spf": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether SPF filtering is enabled for the mailbox. Updated via changeMailboxSpf.",
			},
			"comment": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Free-text comment on the mailbox. Set at creation and updated via updateComment.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (\"<domain>/<name>\").",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *mailboxResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *mailboxResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mailboxModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, name := plan.Domain.ValueString(), plan.Name.ValueString()

	// password is write-only, so it lives in the config, not the plan/state.
	password, ok := r.writeOnlyPassword(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !ok {
		resp.Diagnostics.AddError("Missing mailbox password", "password is required when creating a mailbox")
		return
	}

	// createMbox sets the comment as part of creation; antispam and spf are applied
	// afterwards through their own setters (createMbox takes neither).
	if _, err := r.client.Mail.CreateMbox(ctx, domain, name, password, plan.Comment.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to create mailbox", err.Error())
		return
	}
	if err := r.client.Mail.UpdateAntispamState(ctx, domain, name, antispamLevels[plan.Antispam.ValueString()]); err != nil {
		resp.Diagnostics.AddError("Failed to set mailbox antispam", err.Error())
		return
	}
	if !plan.SPF.IsNull() && !plan.SPF.IsUnknown() {
		if err := r.client.Mail.ChangeMailboxSpf(ctx, domain, name, plan.SPF.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Failed to set mailbox SPF", err.Error())
			return
		}
	}
	// Read back the created mailbox to populate the computed fields (quota, and the
	// server-resolved spf/comment) from the live list.
	mbox, found, err := r.findMailbox(ctx, domain, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read mailbox after create", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Mailbox missing after create", fmt.Sprintf("mailbox %q not found on %q", name, domain))
		return
	}
	r.applyRemote(&plan, mbox)
	plan.ID = types.StringValue(mailboxID(domain, name))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *mailboxResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mailboxModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, name := state.Domain.ValueString(), state.Name.ValueString()
	mbox, found, err := r.findMailbox(ctx, domain, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read mailboxes", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx) // removed outside Terraform
		return
	}
	r.applyRemote(&state, mbox)
	state.ID = types.StringValue(mailboxID(domain, name))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *mailboxResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state mailboxModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, name := plan.Domain.ValueString(), plan.Name.ValueString()

	// Diff each mutable field; call only the setter whose field changed. domain and
	// name force replacement, so they never reach Update.
	//
	// password is write-only (not in state), so it can't be diffed directly; a change
	// to password_wo_version is the signal to read the new password from config and
	// rotate it.
	if !plan.PasswordWOVersion.Equal(state.PasswordWOVersion) {
		password, ok := r.writeOnlyPassword(ctx, req.Config, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if !ok {
			resp.Diagnostics.AddError("Missing mailbox password", "password must be set when password_wo_version changes")
			return
		}
		if err := r.client.Mail.ChangeMailboxPassword(ctx, domain, name, password); err != nil {
			resp.Diagnostics.AddError("Failed to change mailbox password", err.Error())
			return
		}
	}
	if !plan.Antispam.Equal(state.Antispam) {
		if err := r.client.Mail.UpdateAntispamState(ctx, domain, name, antispamLevels[plan.Antispam.ValueString()]); err != nil {
			resp.Diagnostics.AddError("Failed to update mailbox antispam", err.Error())
			return
		}
	}
	if !plan.SPF.Equal(state.SPF) {
		if err := r.client.Mail.ChangeMailboxSpf(ctx, domain, name, plan.SPF.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Failed to change mailbox SPF", err.Error())
			return
		}
	}
	if !plan.Comment.Equal(state.Comment) {
		if err := r.client.Mail.UpdateComment(ctx, domain, name, plan.Comment.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to update mailbox comment", err.Error())
			return
		}
	}
	mbox, found, err := r.findMailbox(ctx, domain, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read mailbox after update", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Mailbox missing after update", fmt.Sprintf("mailbox %q not found on %q", name, domain))
		return
	}
	r.applyRemote(&plan, mbox)
	plan.ID = types.StringValue(mailboxID(domain, name))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *mailboxResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mailboxModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Mail.DropMbox(ctx, state.Domain.ValueString(), state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete mailbox", err.Error())
		return
	}
}

// ImportState accepts "<domain>/<name>". Read then fills password (write-only, so
// left as-is from config) and the computed fields.
func (r *mailboxResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	domain, name, ok := strings.Cut(req.ID, "/")
	if !ok || domain == "" || name == "" {
		resp.Diagnostics.AddError("Invalid import id", "expected \"<domain>/<name>\", got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), domain)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), mailboxID(domain, name))...)
}

// findMailbox lists the domain's mailboxes and returns the one whose local part
// matches name (List-diff identity, like findDNSRecord).
func (r *mailboxResource) findMailbox(ctx context.Context, domain, name string) (mail.Mailbox, bool, error) {
	list, err := r.client.Mail.MailboxesList(ctx, domain, "", mail.ListOptions{})
	if err != nil {
		return mail.Mailbox{}, false, err
	}
	for _, m := range list.List {
		if mailboxLocalPart(m.Mbox) == name {
			return m, true, nil
		}
	}
	return mail.Mailbox{}, false, nil
}

// applyRemote refreshes the computed/mutable fields from a live mailbox. password
// is never API-reported, so it is left untouched (kept from plan/state/config).
func (r *mailboxResource) applyRemote(m *mailboxModel, mbox mail.Mailbox) {
	m.Quota = types.Int64Value(int64(mbox.Quota))
	m.Antispam = types.StringValue(antispamName(int(mbox.Antispam)))
	m.SPF = types.BoolValue(mbox.SPF == 1)
	m.Comment = types.StringValue(mbox.Comment)
}

// writeOnlyPassword reads the write-only password from the request config. Write-only
// values never reach the plan or state, so they must be read from config directly. It
// returns the value and whether it was set to a non-empty string.
func (r *mailboxResource) writeOnlyPassword(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) (string, bool) {
	var pw types.String
	diags.Append(config.GetAttribute(ctx, path.Root("password"), &pw)...)
	if diags.HasError() || pw.IsNull() || pw.ValueString() == "" {
		return "", false
	}
	return pw.ValueString(), true
}

func mailboxID(domain, name string) string { return domain + "/" + name }

// mailboxLocalPart returns the label before @ of a wire mailbox address; the API
// reports the full address (e.g. "info@example.com").
func mailboxLocalPart(mbox string) string {
	if local, _, ok := strings.Cut(mbox, "@"); ok {
		return local
	}
	return mbox
}

// antispamName maps the API's numeric filter level back to its human name; an
// unrecognized level reads as "off".
func antispamName(level int) string {
	for name, l := range antispamLevels {
		if l == level {
			return name
		}
	}
	return "off"
}
