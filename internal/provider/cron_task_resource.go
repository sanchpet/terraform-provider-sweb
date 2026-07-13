package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/vh/cron"
)

var (
	_ resource.Resource                = (*cronTaskResource)(nil)
	_ resource.ResourceWithImportState = (*cronTaskResource)(nil)
)

// NewCronTaskResource is the resource factory registered with the provider.
func NewCronTaskResource() resource.Resource { return &cronTaskResource{} }

// cronTaskResource manages one shared-hosting crontab entry (addTask/removeTask +
// getTasks on /vh/cron). A cron task has no stable server id — its identity is its
// content: the five schedule positions plus the command line. The API models
// editTask as "replace the whole line", so every field forces replacement and the
// resource id is the raw crontab line the API reports (Task.Task), which is also
// removeTask's key.
//
// The SDK's Schedule carries the five positions as plain integers, so this
// resource expresses only numeric single-value schedules (no "*", ranges, or
// steps) — the API surface itself takes integers.
type cronTaskResource struct{ client *sweb.Client }

type cronTaskModel struct {
	Minute  types.Int64  `tfsdk:"minute"`
	Hour    types.Int64  `tfsdk:"hour"`
	Day     types.Int64  `tfsdk:"day"`
	Month   types.Int64  `tfsdk:"month"`
	Weekday types.Int64  `tfsdk:"weekday"`
	Command types.String `tfsdk:"command"`
	ID      types.String `tfsdk:"id"`
}

func (r *cronTaskResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cron_task"
}

func (r *cronTaskResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	forceNewInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	posAttr := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{Required: true, Description: desc, PlanModifiers: forceNewInt}
	}
	resp.Schema = schema.Schema{
		Description: "Manages one shared-hosting crontab entry (addTask/removeTask on /vh/cron). Identity is the " +
			"entry's content — the five schedule positions plus the command — so every field forces replacement. " +
			"Only numeric single-value schedules are expressible (the API takes integers, not \"*\" or ranges).",
		Attributes: map[string]schema.Attribute{
			"minute":  posAttr("Minute position, 0..59."),
			"hour":    posAttr("Hour position, 0..23."),
			"day":     posAttr("Day-of-month position, 1..31."),
			"month":   posAttr("Month position, 0..12."),
			"weekday": posAttr("Day-of-week position, 0..7 (0 and 7 both mean Sunday)."),
			"command": schema.StringAttribute{
				Required:      true,
				Description:   "The command line to run.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id — the raw crontab line the API reports (removeTask's key).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *cronTaskResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *cronTaskResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cronTaskModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	sc := scheduleOf(plan)
	if err := r.client.Cron.AddTask(ctx, sc); err != nil {
		resp.Diagnostics.AddError("Failed to create cron task", err.Error())
		return
	}
	// addTask returns no id, so correlate the new entry by matching its content
	// (schedule + command) in the live list, then adopt the API's raw line as id.
	task, found, err := r.findTask(ctx, func(t cron.Task) bool { return taskMatchesSchedule(t, sc) })
	if err != nil {
		resp.Diagnostics.AddError("Failed to read cron task after create", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Cron task missing after create", "the created task was not found in getTasks")
		return
	}
	plan.ID = types.StringValue(task.Task)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cronTaskResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cronTaskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	task, found, err := r.findTask(ctx, func(t cron.Task) bool { return t.Task == id })
	if err != nil {
		resp.Diagnostics.AddError("Failed to read cron tasks", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx) // removed outside Terraform
		return
	}
	applyTask(&state, task)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is unreachable: every attribute forces replacement. It is present only
// to satisfy the resource.Resource interface.
func (r *cronTaskResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected update", "all cron_task attributes force replacement; Update should never run")
}

func (r *cronTaskResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cronTaskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Cron.RemoveTask(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete cron task", err.Error())
		return
	}
}

// ImportState accepts the raw crontab line (Task.Task, e.g. "30 12 1 12 7 cmd").
// Read then repopulates the schedule fields from the matching live entry.
func (r *cronTaskResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// findTask returns the first live cron task satisfying match.
func (r *cronTaskResource) findTask(ctx context.Context, match func(cron.Task) bool) (cron.Task, bool, error) {
	tasks, err := r.client.Cron.GetTasks(ctx)
	if err != nil {
		return cron.Task{}, false, err
	}
	for _, t := range tasks {
		if match(t) {
			return t, true, nil
		}
	}
	return cron.Task{}, false, nil
}

// scheduleOf builds the SDK Schedule from a plan/state model.
func scheduleOf(m cronTaskModel) cron.Schedule {
	return cron.Schedule{
		Minute:  int(m.Minute.ValueInt64()),
		Hour:    int(m.Hour.ValueInt64()),
		Day:     int(m.Day.ValueInt64()),
		Month:   int(m.Month.ValueInt64()),
		Weekday: int(m.Weekday.ValueInt64()),
		Command: m.Command.ValueString(),
	}
}

// taskMatchesSchedule reports whether a live task has the given schedule and
// command — the content identity used to correlate a just-created task.
func taskMatchesSchedule(t cron.Task, sc cron.Schedule) bool {
	return int(t.Minute) == sc.Minute && int(t.Hour) == sc.Hour && int(t.Day) == sc.Day &&
		int(t.Month) == sc.Month && int(t.Weekday) == sc.Weekday && t.Command == sc.Command
}

// applyTask refreshes the model's schedule fields from a live task.
func applyTask(m *cronTaskModel, t cron.Task) {
	m.Minute = types.Int64Value(int64(t.Minute))
	m.Hour = types.Int64Value(int64(t.Hour))
	m.Day = types.Int64Value(int64(t.Day))
	m.Month = types.Int64Value(int64(t.Month))
	m.Weekday = types.Int64Value(int64(t.Weekday))
	m.Command = types.StringValue(t.Command)
	m.ID = types.StringValue(t.Task)
}
