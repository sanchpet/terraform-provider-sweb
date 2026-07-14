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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/balancer"
	"github.com/sanchpet/sweb-go-sdk/flex"
)

var (
	_ resource.Resource                = (*balancerResource)(nil)
	_ resource.ResourceWithImportState = (*balancerResource)(nil)
)

// NewBalancerResource is the resource factory registered with the provider.
func NewBalancerResource() resource.Resource { return &balancerResource{} }

// balancerResource manages a cloud load balancer (endpoint /balancer:
// create/index/edit/remove). Like the VPS, create returns nothing usable, so the
// new node is correlated by diffing the balancer list on BillingID before and
// after; creates are serialized behind createMu; the create/edit lifecycle is
// asynchronous (Balancer.CurrentAction), so both poll until the balancer idles.
type balancerResource struct{ client *sweb.Client }

// balancerModel is the Terraform state/plan model for a sweb_balancer.
type balancerModel struct {
	// Inputs.
	Datacenter  types.Int64  `tfsdk:"datacenter"` // ForceNew
	Type        types.String `tfsdk:"type"`       // roundrobin|leastconn, updatable
	PlanID      types.Int64  `tfsdk:"plan_id"`    // ForceNew
	Alias       types.String `tfsdk:"alias"`      // updatable
	HealthCheck types.Bool   `tfsdk:"health_check"`
	ProxyProto  types.Bool   `tfsdk:"proxy_proto"`
	Keepalive   types.Bool   `tfsdk:"keepalive"`
	SaveSession types.Bool   `tfsdk:"save_session"`

	Servers []balancerServerModel `tfsdk:"server"`
	Rules   []balancerRuleModel   `tfsdk:"rule"`

	// Computed.
	ID         types.String `tfsdk:"id"` // = billing_id
	BillingID  types.String `tfsdk:"billing_id"`
	PlanName   types.String `tfsdk:"plan_name"`
	Price      types.Int64  `tfsdk:"price"`
	Active     types.Bool   `tfsdk:"active"`
	IPBalancer types.String `tfsdk:"ip_balancer"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// balancerServerModel is one back-end server block. Weight applies only to
// roundrobin (1..5); vps_name is the API-reported label for a VPS target.
type balancerServerModel struct {
	IP      types.String `tfsdk:"ip"`
	Weight  types.Int64  `tfsdk:"weight"`
	VPSName types.String `tfsdk:"vps_name"`
}

// balancerRuleModel is one forwarding rule block: front-end (balancer)
// protocol/port to back-end (server) protocol/port.
type balancerRuleModel struct {
	ProtocolBalancer types.String `tfsdk:"protocol_balancer"`
	PortBalancer     types.String `tfsdk:"port_balancer"`
	ProtocolServer   types.String `tfsdk:"protocol_server"`
	PortServer       types.String `tfsdk:"port_server"`
}

func (r *balancerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_balancer"
}

func (r *balancerResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	// keep = reuse the prior state value when the planned value is unknown, for
	// computed fields the only in-place op (edit) does not change — keeps them out
	// of the plan (no "known after apply" noise); Read still refreshes them.
	keepStr := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		Description: "A SpaceWeb cloud load balancer. type, alias, the health_check/proxy_proto/keepalive/" +
			"save_session toggles and the server/rule sets update in place via edit; datacenter and plan_id " +
			"force replacement.",
		Attributes: map[string]schema.Attribute{
			"datacenter": schema.Int64Attribute{
				Required:      true,
				Description:   "Datacenter id (1=spb, 2=msk, 3=ams). Forces replacement.",
				PlanModifiers: replaceInt,
			},
			"type": schema.StringAttribute{
				Required:    true,
				Description: "Balancing algorithm: roundrobin or leastconn. Updated in place via edit.",
				Validators:  []validator.String{stringvalidator.OneOf("roundrobin", "leastconn")},
			},
			"plan_id": schema.Int64Attribute{
				Required:      true,
				Description:   "Tariff plan id. Forces replacement.",
				PlanModifiers: replaceInt,
			},
			"alias": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Human-facing name for the balancer. Updated in place via edit.",
			},
			"health_check": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether back-end health checks are enabled. Updated in place.",
			},
			"proxy_proto": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the PROXY protocol is enabled. Updated in place.",
			},
			"keepalive": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether keepalive is enabled. Updated in place.",
			},
			"save_session": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether session persistence (sticky sessions) is enabled. Updated in place.",
			},

			// Computed.
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Terraform identifier — equals billing_id.",
				PlanModifiers: keepStr,
			},
			"billing_id": schema.StringAttribute{
				Computed:      true,
				Description:   "SpaceWeb service id; the key used for edit, delete and import.",
				PlanModifiers: keepStr,
			},
			"plan_name": schema.StringAttribute{Computed: true, Description: "Human plan name reported by the API."},
			"price":     schema.Int64Attribute{Computed: true, Description: "Monthly price reported by the API."},
			"active":    schema.BoolAttribute{Computed: true, Description: "Whether the balancer is active."},
			"ip_balancer": schema.StringAttribute{
				Computed:      true,
				Description:   "The balancer's public IP address.",
				PlanModifiers: keepStr,
			},
		},
		Blocks: map[string]schema.Block{
			"server": schema.ListNestedBlock{
				Description: "A back-end server behind the balancer (max 20). Updated in place via edit.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"ip": schema.StringAttribute{
							Required:    true,
							Description: "The back-end server IP address.",
						},
						"weight": schema.Int64Attribute{
							Optional:    true,
							Computed:    true,
							Description: "Server weight (1..5); roundrobin only, ignored otherwise.",
						},
						"vps_name": schema.StringAttribute{
							Computed:    true,
							Description: "API-reported VPS label for the target, empty for a bare IP.",
						},
					},
				},
			},
			"rule": schema.ListNestedBlock{
				Description: "A forwarding rule: front-end (balancer) protocol/port to back-end (server) protocol/port.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"protocol_balancer": schema.StringAttribute{
							Required:    true,
							Description: "Front-end (balancer) protocol, e.g. tcp, http, https.",
						},
						"port_balancer": schema.StringAttribute{
							Required:    true,
							Description: "Front-end (balancer) port.",
						},
						"protocol_server": schema.StringAttribute{
							Required:    true,
							Description: "Back-end (server) protocol.",
						},
						"port_server": schema.StringAttribute{
							Required:    true,
							Description: "Back-end (server) port.",
						},
					},
				},
			},
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true}),
		},
	}
}

func (r *balancerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*sweb.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *sweb.Client, got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *balancerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan balancerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := balancer.CreateOptions{
		Datacenter:   int(plan.Datacenter.ValueInt64()),
		Type:         plan.Type.ValueString(),
		Servers:      serversFromModel(plan.Servers),
		Rules:        rulesFromModel(plan.Rules),
		PlanID:       int(plan.PlanID.ValueInt64()),
		HealthCheck:  plan.HealthCheck.ValueBool(),
		ProxyProto:   plan.ProxyProto.ValueBool(),
		Keepalive:    plan.Keepalive.ValueBool(),
		SaveSession:  plan.SaveSession.ValueBool(),
		Alias:        plan.Alias.ValueString(),
		IsFirstOrder: true,
	}

	// The snapshot → create → correlate window must be serial: the List-diff that
	// identifies the new balancer is only unambiguous when a single create runs at
	// a time. createMu is the package-wide create mutex shared with the VPS
	// resource (see its definition in vps_resource.go).
	var node balancer.Balancer
	ok := func() bool {
		createMu.Lock()
		defer createMu.Unlock()

		before, err := r.listBillingIDs(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list balancers before create", err.Error())
			return false
		}
		if err := r.client.Balancer.Create(ctx, opts); err != nil {
			resp.Diagnostics.AddError("Failed to create balancer", err.Error())
			return false
		}
		n, err := r.waitForNewBalancer(ctx, before, createTimeout)
		if err != nil {
			resp.Diagnostics.AddError("Balancer did not become ready", err.Error())
			return false
		}
		node = n
		return true
	}()
	if !ok {
		return
	}

	r.applyAPIState(&plan, node)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *balancerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state balancerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	node, err := r.findByBillingID(ctx, state.BillingID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read balancer", err.Error())
		return
	}
	if node == nil {
		resp.State.RemoveResource(ctx) // gone server-side → drop from state
		return
	}

	r.applyAPIState(&state, *node)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update applies the in-place changes via edit: type, alias, the boolean toggles
// and the server/rule sets. datacenter and plan_id are RequiresReplace, so
// Terraform only routes here for the in-place set. An edit is asynchronous, so it
// waits until current_action idles.
func (r *balancerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state balancerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, diags := plan.Timeouts.Update(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	billingID := state.BillingID.ValueString()
	opts := balancer.EditOptions{
		BillingID:   billingID,
		Type:        plan.Type.ValueString(),
		Servers:     serversFromModel(plan.Servers),
		Rules:       rulesFromModel(plan.Rules),
		HealthCheck: plan.HealthCheck.ValueBool(),
		ProxyProto:  plan.ProxyProto.ValueBool(),
		Keepalive:   plan.Keepalive.ValueBool(),
		SaveSession: plan.SaveSession.ValueBool(),
		Alias:       plan.Alias.ValueString(),
	}
	if err := r.client.Balancer.Edit(ctx, opts); err != nil {
		resp.Diagnostics.AddError("Failed to edit balancer", err.Error())
		return
	}
	if _, err := r.waitForIdle(ctx, billingID, updateTimeout); err != nil {
		resp.Diagnostics.AddError("Balancer edit did not settle", err.Error())
		return
	}

	node, err := r.findByBillingID(ctx, billingID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read balancer after update", err.Error())
		return
	}
	if node == nil {
		resp.Diagnostics.AddError("Balancer not found after update", fmt.Sprintf("no balancer with billing_id %q", billingID))
		return
	}
	r.applyAPIState(&plan, *node)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *balancerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state balancerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Balancer.Remove(ctx, state.BillingID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete balancer", err.Error())
		return
	}
}

// ImportState imports a balancer by billing_id. Read then fills every field from
// the live list.
func (r *balancerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("billing_id"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// --- helpers ---

// applyAPIState copies the server-reported fields of node into m, including the
// server and rule sets (the API is the source of truth for weight/vps_name).
func (r *balancerResource) applyAPIState(m *balancerModel, node balancer.Balancer) {
	m.ID = types.StringValue(node.BillingID)
	m.BillingID = types.StringValue(node.BillingID)
	m.Datacenter = types.Int64Value(int64(node.Datacenter))
	m.Type = types.StringValue(node.Type)
	m.PlanID = types.Int64Value(int64(node.PlanID))
	m.PlanName = types.StringValue(node.PlanName)
	m.Price = types.Int64Value(int64(node.Price))
	m.Active = types.BoolValue(node.Active)
	m.IPBalancer = types.StringValue(node.IPBalancer)
	m.Alias = types.StringValue(node.Name)
	m.HealthCheck = types.BoolValue(node.HealthCheck)
	m.ProxyProto = types.BoolValue(node.ProxyProto)
	m.Keepalive = types.BoolValue(node.Keepalive)
	m.SaveSession = types.BoolValue(node.SaveSession)
	m.Servers = serversToModel(node.Servers)
	m.Rules = rulesToModel(node.Rules)
}

func serversFromModel(servers []balancerServerModel) []balancer.Server {
	out := make([]balancer.Server, 0, len(servers))
	for _, s := range servers {
		out = append(out, balancer.Server{
			IP:      s.IP.ValueString(),
			Weight:  flex.Int(s.Weight.ValueInt64()),
			VPSName: s.VPSName.ValueString(),
		})
	}
	return out
}

func serversToModel(servers []balancer.Server) []balancerServerModel {
	out := make([]balancerServerModel, 0, len(servers))
	for _, s := range servers {
		out = append(out, balancerServerModel{
			IP:      types.StringValue(s.IP),
			Weight:  types.Int64Value(int64(s.Weight)),
			VPSName: types.StringValue(s.VPSName),
		})
	}
	return out
}

func rulesFromModel(rules []balancerRuleModel) []balancer.Rule {
	out := make([]balancer.Rule, 0, len(rules))
	for _, ru := range rules {
		out = append(out, balancer.Rule{
			ProtocolBalancer: ru.ProtocolBalancer.ValueString(),
			PortBalancer:     ru.PortBalancer.ValueString(),
			ProtocolServer:   ru.ProtocolServer.ValueString(),
			PortServer:       ru.PortServer.ValueString(),
		})
	}
	return out
}

func rulesToModel(rules []balancer.Rule) []balancerRuleModel {
	out := make([]balancerRuleModel, 0, len(rules))
	for _, ru := range rules {
		out = append(out, balancerRuleModel{
			ProtocolBalancer: types.StringValue(ru.ProtocolBalancer),
			PortBalancer:     types.StringValue(ru.PortBalancer),
			ProtocolServer:   types.StringValue(ru.ProtocolServer),
			PortServer:       types.StringValue(ru.PortServer),
		})
	}
	return out
}

func (r *balancerResource) listBillingIDs(ctx context.Context) (map[string]struct{}, error) {
	list, err := r.client.Balancer.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(list))
	for _, b := range list {
		ids[b.BillingID] = struct{}{}
	}
	return ids, nil
}

func (r *balancerResource) findByBillingID(ctx context.Context, billingID string) (*balancer.Balancer, error) {
	list, err := r.client.Balancer.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].BillingID == billingID {
			return &list[i], nil
		}
	}
	return nil, nil
}

// waitForNewBalancer polls List until a billing id not present in `before`
// appears and its current_action is idle, or ctx/timeout is exhausted.
func (r *balancerResource) waitForNewBalancer(ctx context.Context, before map[string]struct{}, timeout time.Duration) (balancer.Balancer, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var last balancer.Balancer
	found := false
	for {
		list, err := r.client.Balancer.List(ctx)
		if err == nil {
			for _, b := range list {
				if _, existed := before[b.BillingID]; existed {
					continue
				}
				last, found = b, true
				if b.CurrentAction == "" {
					return b, nil
				}
			}
		}

		select {
		case <-ctx.Done():
			if found {
				return last, nil // appeared but not yet idle — return what we have
			}
			return balancer.Balancer{}, fmt.Errorf("timed out waiting for the new balancer to appear")
		case <-ticker.C:
		}
	}
}

// waitForIdle polls List until the balancer with billingID reports an idle
// current_action, or ctx/timeout is exhausted.
func (r *balancerResource) waitForIdle(ctx context.Context, billingID string, timeout time.Duration) (balancer.Balancer, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		node, err := r.findByBillingID(ctx, billingID)
		if err == nil && node != nil && node.CurrentAction == "" {
			return *node, nil
		}

		select {
		case <-ctx.Done():
			return balancer.Balancer{}, fmt.Errorf("timed out waiting for balancer %q to idle", billingID)
		case <-ticker.C:
		}
	}
}
