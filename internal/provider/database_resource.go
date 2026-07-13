package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	sweb "github.com/sanchpet/sweb-go-sdk"
	"github.com/sanchpet/sweb-go-sdk/vh/hosting"
)

var (
	_ resource.Resource                = (*databaseResource)(nil)
	_ resource.ResourceWithImportState = (*databaseResource)(nil)
)

// NewDatabaseResource is the resource factory registered with the provider.
func NewDatabaseResource() resource.Resource { return &databaseResource{} }

// databaseResource manages one shared-hosting MySQL database (databaseMysqlCreate/
// databaseMysqlDelete + databaseGetList on /vh/hosting). Identity is the database
// `name` as supplied at creation; the API may store it under an account-prefixed
// full name (exposed as the computed `full_name`), which is the key every
// post-create call uses. `name` and `version` force replacement; `password`
// (databaseMysqlChangePass) and `comment` (databaseEditComment) update in place.
type databaseResource struct{ client *sweb.Client }

type databaseModel struct {
	Name        types.String  `tfsdk:"name"`
	Password    types.String  `tfsdk:"password"`
	Comment     types.String  `tfsdk:"comment"`
	Version     types.String  `tfsdk:"version"`
	FullName    types.String  `tfsdk:"full_name"`
	Login       types.String  `tfsdk:"login"`
	Charset     types.String  `tfsdk:"charset"`
	SizeTables  types.Float64 `tfsdk:"size_tables"`
	CountTables types.Int64   `tfsdk:"count_tables"`
	ID          types.String  `tfsdk:"id"`
}

func (r *databaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

func (r *databaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	forceNewStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	useState := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "Manages one shared-hosting MySQL database (databaseMysqlCreate/databaseMysqlDelete on " +
			"/vh/hosting). Identity is the database `name`; the API may store it under an account-prefixed " +
			"`full_name`. `name` and `version` force replacement; `password` and `comment` update in place.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "The database name as supplied at creation (the create key and resource id).",
				PlanModifiers: forceNewStr,
			},
			"password": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "The database user password. Updated in place via databaseMysqlChangePass.",
			},
			"comment": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Free-text comment. Set at creation and updated via databaseEditComment.",
			},
			"version": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "MySQL version. Set at creation only (forces replacement); defaulted by the API when omitted.",
				PlanModifiers: append(forceNewStr, useState...),
			},
			"full_name": schema.StringAttribute{
				Computed:      true,
				Description:   "The database name as the API stores it (possibly account-prefixed); the post-create call key.",
				PlanModifiers: useState,
			},
			"login": schema.StringAttribute{
				Computed:      true,
				Description:   "The database login reported by the API.",
				PlanModifiers: useState,
			},
			"charset": schema.StringAttribute{
				Computed:    true,
				Description: "The database character set reported by the API.",
			},
			"size_tables": schema.Float64Attribute{
				Computed:    true,
				Description: "Total table size in MB.",
			},
			"count_tables": schema.Int64Attribute{
				Computed:    true,
				Description: "Number of tables in the database.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (equal to name).",
				PlanModifiers: useState,
			},
		},
	}
}

func (r *databaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *databaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	if err := r.client.HostingDB.MysqlCreate(ctx, hosting.MysqlCreateOptions{
		Name:     name,
		Password: plan.Password.ValueString(),
		Comment:  plan.Comment.ValueString(),
		Version:  plan.Version.ValueString(),
	}); err != nil {
		resp.Diagnostics.AddError("Failed to create database", err.Error())
		return
	}
	db, found, err := r.findDatabase(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read database after create", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Database missing after create", fmt.Sprintf("database %q not found after create", name))
		return
	}
	applyDatabase(&plan, db)
	plan.ID = types.StringValue(name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *databaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	db, found, err := r.findDatabase(ctx, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read databases", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx) // removed outside Terraform
		return
	}
	applyDatabase(&state, db)
	state.ID = types.StringValue(state.Name.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *databaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Post-create calls key on the API's stored full name, carried in state.
	fullName := state.FullName.ValueString()
	if !plan.Password.Equal(state.Password) {
		if err := r.client.HostingDB.MysqlChangePass(ctx, fullName, plan.Password.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to change database password", err.Error())
			return
		}
	}
	if !plan.Comment.Equal(state.Comment) {
		if err := r.client.HostingDB.EditComment(ctx, "mysql", fullName, plan.Comment.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to update database comment", err.Error())
			return
		}
	}
	db, found, err := r.findDatabase(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read database after update", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Database missing after update", fmt.Sprintf("database %q not found after update", plan.Name.ValueString()))
		return
	}
	applyDatabase(&plan, db)
	plan.ID = types.StringValue(plan.Name.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *databaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.HostingDB.MysqlDelete(ctx, state.FullName.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete database", err.Error())
		return
	}
}

// ImportState accepts the database name. Read then resolves full_name and the
// computed fields; password is write-only, so it stays as configured.
func (r *databaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// findDatabase lists MySQL databases and returns the one whose name matches — by
// exact name or by the account-prefixed "<login>_<name>" form the API may store.
func (r *databaseResource) findDatabase(ctx context.Context, name string) (hosting.Database, bool, error) {
	list, err := r.client.HostingDB.DatabaseList(ctx, hosting.ListOptions{})
	if err != nil {
		return hosting.Database{}, false, err
	}
	for _, db := range list.List {
		if db.Type != "mysql" {
			continue
		}
		if db.Name == name || strings.HasSuffix(db.Name, "_"+name) {
			return db, true, nil
		}
	}
	return hosting.Database{}, false, nil
}

// applyDatabase refreshes the computed/mutable fields from a live database.
// password is never API-reported, so it is left untouched.
func applyDatabase(m *databaseModel, db hosting.Database) {
	m.FullName = types.StringValue(db.Name)
	m.Login = types.StringValue(db.Login)
	m.Charset = types.StringValue(db.Charset)
	m.Comment = types.StringValue(db.Comment)
	m.Version = types.StringValue(db.Version)
	m.SizeTables = types.Float64Value(float64(db.SizeTables))
	m.CountTables = types.Int64Value(int64(db.CountTables))
}
