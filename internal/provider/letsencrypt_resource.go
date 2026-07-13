package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/vh/ssl"
)

var (
	_ resource.Resource                = (*letsEncryptResource)(nil)
	_ resource.ResourceWithImportState = (*letsEncryptResource)(nil)
)

// NewLetsEncryptResource is the resource factory registered with the provider.
func NewLetsEncryptResource() resource.Resource { return &letsEncryptResource{} }

// letsEncryptResource manages a free Let's Encrypt certificate on shared hosting
// (installLetsEncrypt/removeCertificate + index/editAutoprolong on /vh/ssl). A
// certificate has no client-supplied id — it is identified by the `domain` it
// covers; the issuance inputs (wildcard/virtdom/ip/challenge) force replacement,
// while `autoprolong` toggles in place. Issuance is asynchronous, so Create polls
// the certificate list until the certificate appears (bounded by the create
// timeout).
type letsEncryptResource struct{ client *sweb.Client }

type letsEncryptModel struct {
	Domain        types.String   `tfsdk:"domain"`
	Wildcard      types.Bool     `tfsdk:"wildcard"`
	Virtdom       types.String   `tfsdk:"virtdom"`
	IP            types.String   `tfsdk:"ip"`
	Challenge     types.String   `tfsdk:"challenge"`
	Autoprolong   types.Bool     `tfsdk:"autoprolong"`
	CertificateID types.Int64    `tfsdk:"certificate_id"`
	Status        types.String   `tfsdk:"status"`
	ValidTo       types.String   `tfsdk:"valid_to"`
	ID            types.String   `tfsdk:"id"`
	Timeouts      timeouts.Value `tfsdk:"timeouts"`
}

func (r *letsEncryptResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_letsencrypt"
}

func (r *letsEncryptResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	forceNewStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages a free Let's Encrypt certificate on shared hosting (installLetsEncrypt/removeCertificate " +
			"on /vh/ssl). Identified by the `domain` it covers; the issuance inputs force replacement, while " +
			"`autoprolong` toggles in place. Issuance is asynchronous — Create waits for the certificate to appear.",
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				Required:      true,
				Description:   "The fully-qualified domain the certificate covers (its identity).",
				PlanModifiers: forceNewStr,
			},
			"wildcard": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				Description:   "Request a wildcard certificate. Set at issuance (forces replacement).",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"virtdom": schema.StringAttribute{
				Optional:      true,
				Description:   "Subdomain to cover, e.g. \"sub.example.com\". Set at issuance (forces replacement).",
				PlanModifiers: forceNewStr,
			},
			"ip": schema.StringAttribute{
				Optional:      true,
				Description:   "Target IP, or \"sni\" for SNI. Set at issuance (forces replacement).",
				PlanModifiers: forceNewStr,
			},
			"challenge": schema.StringAttribute{
				Optional:      true,
				Description:   "Validation type: \"acme\" or \"dns\". Set at issuance (forces replacement).",
				PlanModifiers: forceNewStr,
				Validators:    []validator.String{stringvalidator.OneOf("acme", "dns")},
			},
			"autoprolong": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether auto-prolongation is enabled. Toggled in place via editAutoprolong.",
			},
			"certificate_id": schema.Int64Attribute{
				Computed:    true,
				Description: "The API's numeric certificate id (the removeCertificate/editAutoprolong key).",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "The certificate status reported by the API.",
			},
			"valid_to": schema.StringAttribute{
				Computed:    true,
				Description: "The certificate expiry date reported by the API.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (equal to domain).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *letsEncryptResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *letsEncryptResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan letsEncryptModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := plan.Domain.ValueString()
	if _, err := r.client.VHSSL.InstallLetsEncrypt(ctx, domain, plan.Wildcard.ValueBool(), &ssl.InstallLetsEncryptOptions{
		Virtdom:   plan.Virtdom.ValueString(),
		IP:        plan.IP.ValueString(),
		Challenge: plan.Challenge.ValueString(),
	}); err != nil {
		resp.Diagnostics.AddError("Failed to install Let's Encrypt certificate", err.Error())
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	cert, err := r.waitForCertificate(ctx, domain, createTimeout)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read certificate after install", err.Error())
		return
	}

	// autoprolong is Optional+Computed: apply the requested value only when the
	// user set it and it differs from what the freshly-issued certificate reports.
	if !plan.Autoprolong.IsNull() && !plan.Autoprolong.IsUnknown() && plan.Autoprolong.ValueBool() != cert.Autoprolong {
		if _, err := r.client.VHSSL.EditAutoprolong(ctx, int(cert.ID), plan.Autoprolong.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Failed to set certificate auto-prolongation", err.Error())
			return
		}
		cert.Autoprolong = plan.Autoprolong.ValueBool()
	}
	applyCert(&plan, cert)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *letsEncryptResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state letsEncryptModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cert, found, err := r.findCertificate(ctx, state.Domain.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read certificates", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx) // removed outside Terraform
		return
	}
	applyCert(&state, cert)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *letsEncryptResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state letsEncryptModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Only autoprolong can change; every issuance input forces replacement.
	if !plan.Autoprolong.Equal(state.Autoprolong) {
		if _, err := r.client.VHSSL.EditAutoprolong(ctx, int(state.CertificateID.ValueInt64()), plan.Autoprolong.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Failed to update certificate auto-prolongation", err.Error())
			return
		}
	}
	cert, found, err := r.findCertificate(ctx, plan.Domain.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read certificate after update", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Certificate missing after update", fmt.Sprintf("certificate for %q not found", plan.Domain.ValueString()))
		return
	}
	applyCert(&plan, cert)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *letsEncryptResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state letsEncryptModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.VHSSL.RemoveCertificate(ctx, int(state.CertificateID.ValueInt64())); err != nil {
		resp.Diagnostics.AddError("Failed to delete certificate", err.Error())
		return
	}
}

// ImportState accepts the domain. Read then resolves certificate_id and the
// computed fields; the issuance inputs aren't recoverable from the list, so they
// stay as configured.
func (r *letsEncryptResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// findCertificate lists certificates and returns the one covering domain.
func (r *letsEncryptResource) findCertificate(ctx context.Context, domain string) (ssl.Certificate, bool, error) {
	list, err := r.client.VHSSL.List(ctx, nil)
	if err != nil {
		return ssl.Certificate{}, false, err
	}
	for _, c := range list.List {
		if c.Domain == domain {
			return c, true, nil
		}
	}
	return ssl.Certificate{}, false, nil
}

// waitForCertificate polls the certificate list until one covering domain appears,
// or ctx/timeout is exhausted (issuance is asynchronous).
func (r *letsEncryptResource) waitForCertificate(ctx context.Context, domain string, timeout time.Duration) (ssl.Certificate, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		if cert, found, err := r.findCertificate(ctx, domain); err == nil && found {
			return cert, nil
		}
		select {
		case <-ctx.Done():
			return ssl.Certificate{}, fmt.Errorf("timed out waiting for the certificate for %q to be issued", domain)
		case <-ticker.C:
		}
	}
}

// applyCert refreshes the computed/mutable fields from a live certificate.
func applyCert(m *letsEncryptModel, c ssl.Certificate) {
	m.Autoprolong = types.BoolValue(c.Autoprolong)
	m.CertificateID = types.Int64Value(int64(c.ID))
	m.Status = types.StringValue(c.Status)
	m.ValidTo = types.StringValue(c.ValidTo)
	m.ID = types.StringValue(c.Domain)
}
