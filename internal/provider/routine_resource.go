package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/blossomstack/terraform-provider-horsie/internal/client"
	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

var (
	_ resource.Resource                = (*routineResource)(nil)
	_ resource.ResourceWithConfigure   = (*routineResource)(nil)
	_ resource.ResourceWithImportState = (*routineResource)(nil)
)

type routineResource struct{ client *client.Client }

// NewRoutineResource registers `horsie_routine`.
func NewRoutineResource() resource.Resource { return &routineResource{} }

// scheduleModel is HCL's flattening of horsie's `RoutineSchedule` union.
//
// The wire form is a real sum type — "every, with no interval" cannot be
// expressed — but HCL has no sum types, so this is a discriminator plus the
// fields each variant needs, validated in the provider. Sending the wrong
// combination is caught here rather than as a 422 halfway through an apply.
type scheduleModel struct {
	Type       types.String `tfsdk:"type"`
	IntervalS  types.Int64  `tfsdk:"interval_seconds"`
	AtMS       types.Int64  `tfsdk:"at_ms"`
	Timezone   types.String `tfsdk:"timezone"`
	Hour       types.Int64  `tfsdk:"hour"`
	Minute     types.Int64  `tfsdk:"minute"`
	Weekdays   types.List   `tfsdk:"weekdays"`
	DayOfMonth types.Int64  `tfsdk:"day_of_month"`
	Month      types.Int64  `tfsdk:"month"`
}

// environmentSpecModel flattens `EnvironmentSpec`, the other union on this
// resource. Same shape as the schedule: a discriminator plus the fields each
// variant needs, validated here rather than as a 422 mid-apply.
type environmentSpecModel struct {
	Type   types.String `tfsdk:"type"`
	Vendor types.String `tfsdk:"vendor"`
	Name   types.String `tfsdk:"name"`
	Repos  []repoModel  `tfsdk:"repos"`
}

type routineModel struct {
	Name        types.String          `tfsdk:"name"`
	Description types.String          `tfsdk:"description"`
	Agent       types.String          `tfsdk:"agent"`
	Prompt      types.String          `tfsdk:"prompt"`
	Enabled     types.Bool            `tfsdk:"enabled"`
	Schedule    *scheduleModel        `tfsdk:"schedule"`
	Environment *environmentSpecModel `tfsdk:"environment"`
	NextRunAtMS types.Int64           `tfsdk:"next_run_at_ms"`
}

func (r *routineResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_routine"
}

func (r *routineResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An agent preset plus a fixed prompt and a trigger.\n\n" +
			"Running one creates an unattended session that works from the prompt alone. Those " +
			"sessions are listed under the routine, never in the session list.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Slug used in API paths. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "What this routine is for. Defaults to empty.",
			},
			"agent": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Name of the `horsie_agent` preset every run is configured from. " +
					"Reference the resource's `name` so Terraform orders the two correctly.",
			},
			"prompt": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The message queued as each run's first user message.",
			},
			"enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "False pauses the timer. Triggering by hand still works, " +
					"which is why this pauses rather than disables.",
			},
			"next_run_at_ms": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "When the timer fires next, unix epoch millis. Absent when nothing is " +
					"scheduled — a manual routine, a paused one, or a spent `once`.",
			},
		},
		Blocks: map[string]schema.Block{
			"environment": schema.SingleNestedBlock{
				MarkdownDescription: "Where each run happens and what it runs against. **Required** — " +
					"horsie has no default, because an optional environment would be a second, invisible " +
					"way to answer the question, settled by a server default nobody asked for.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "`runtime` for an ad-hoc environment, or `named` for a `horsie_environment` by name.",
					},
					"vendor": schema.StringAttribute{
						Optional: true,
						MarkdownDescription: "`runtime`: the runtime vendor. `local` is spelled here like any other " +
							"vendor — there is no separate variant for it.",
					},
					"name": schema.StringAttribute{
						Optional: true,
						MarkdownDescription: "`named`: the `horsie_environment` to use. It is resolved and snapshotted " +
							"when a run is created, so editing or deleting it never re-points a run that exists.",
					},
				},
				Blocks: map[string]schema.Block{
					"repos": schema.ListNestedBlock{
						MarkdownDescription: "`runtime`: repositories to check out. A vendor that cannot provision a " +
							"workspace rejects a non-empty list at create.",
						NestedObject: schema.NestedBlockObject{
							Attributes: map[string]schema.Attribute{
								"url":     schema.StringAttribute{Required: true, MarkdownDescription: "HTTPS clone URL."},
								"git_ref": schema.StringAttribute{Optional: true, MarkdownDescription: "Branch, tag or commit."},
								"dir":     schema.StringAttribute{Optional: true, MarkdownDescription: "Directory under the workspace."},
							},
						},
					},
				},
			},
			"schedule": schema.SingleNestedBlock{
				MarkdownDescription: "When the routine fires by itself. Omit for a manual routine.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required: true,
						MarkdownDescription: "One of `manual`, `every`, `once`, `daily`, `weekly`, `monthly`, `yearly`. " +
							"Each type reads only the attributes it needs; setting one it does not use is an error " +
							"rather than being silently ignored.",
					},
					"interval_seconds": schema.Int64Attribute{
						Optional: true,
						MarkdownDescription: "`every`: seconds between runs, minimum 60. The next run is scheduled from " +
							"when the previous one fired, so a server that was down resumes with one run rather than a backlog.",
					},
					"at_ms": schema.Int64Attribute{
						Optional: true,
						MarkdownDescription: "`once`: unix epoch millis. An instant already in the past never fires; " +
							"move it forward to re-arm.",
					},
					"timezone": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "`daily`/`weekly`/`monthly`/`yearly`: IANA timezone, e.g. `Europe/London`.",
					},
					"hour":   schema.Int64Attribute{Optional: true, MarkdownDescription: "Hour of day, 0–23."},
					"minute": schema.Int64Attribute{Optional: true, MarkdownDescription: "Minute of hour, 0–59."},
					"weekdays": schema.ListAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "`weekly`: at least one of `Mon`…`Sun`. Duplicates are rejected.",
					},
					"day_of_month": schema.Int64Attribute{
						Optional:            true,
						MarkdownDescription: "`monthly`/`yearly`: day of month. Months without that day are skipped entirely.",
					},
					"month": schema.Int64Attribute{Optional: true, MarkdownDescription: "`yearly`: month, 1–12."},
				},
			},
		},
	}
}

func (r *routineResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	r.client = c
}

// toSchedule turns the flattened block into the union horsie expects, naming
// any attribute that does not belong to the chosen type.
func toSchedule(ctx context.Context, s *scheduleModel) (api.RoutineSchedule, error) {
	if s == nil {
		return api.RoutineSchedule{Variant: api.RoutineScheduleManual{Value: api.ManualSchedule{}}}, nil
	}

	kind := strings.ToLower(s.Type.ValueString())
	set := map[string]bool{
		"interval_seconds": !s.IntervalS.IsNull(),
		"at_ms":            !s.AtMS.IsNull(),
		"timezone":         !s.Timezone.IsNull(),
		"hour":             !s.Hour.IsNull(),
		"minute":           !s.Minute.IsNull(),
		"weekdays":         !s.Weekdays.IsNull(),
		"day_of_month":     !s.DayOfMonth.IsNull(),
		"month":            !s.Month.IsNull(),
	}
	allowed := map[string][]string{
		"manual":  {},
		"every":   {"interval_seconds"},
		"once":    {"at_ms"},
		"daily":   {"timezone", "hour", "minute"},
		"weekly":  {"timezone", "hour", "minute", "weekdays"},
		"monthly": {"timezone", "hour", "minute", "day_of_month"},
		"yearly":  {"timezone", "hour", "minute", "day_of_month", "month"},
	}
	want, ok := allowed[kind]
	if !ok {
		return api.RoutineSchedule{}, fmt.Errorf(
			"unknown schedule type %q (expected manual, every, once, daily, weekly, monthly or yearly)", s.Type.ValueString())
	}
	permitted := map[string]bool{}
	for _, a := range want {
		permitted[a] = true
	}
	var stray, missing []string
	for attr, isSet := range set {
		if isSet && !permitted[attr] {
			stray = append(stray, attr)
		}
	}
	for _, a := range want {
		if !set[a] {
			missing = append(missing, a)
		}
	}
	if len(stray) > 0 {
		return api.RoutineSchedule{}, fmt.Errorf("schedule type %q does not use: %s", kind, strings.Join(stray, ", "))
	}
	if len(missing) > 0 {
		return api.RoutineSchedule{}, fmt.Errorf("schedule type %q needs: %s", kind, strings.Join(missing, ", "))
	}

	tz := s.Timezone.ValueString()
	hour := uint32(s.Hour.ValueInt64())
	minute := uint32(s.Minute.ValueInt64())

	switch kind {
	case "manual":
		return api.RoutineSchedule{Variant: api.RoutineScheduleManual{Value: api.ManualSchedule{}}}, nil
	case "every":
		return api.RoutineSchedule{Variant: api.RoutineScheduleEvery{
			Value: api.EverySchedule{IntervalSecs: uint64(s.IntervalS.ValueInt64())},
		}}, nil
	case "once":
		return api.RoutineSchedule{Variant: api.RoutineScheduleOnce{
			Value: api.OnceSchedule{AtMs: uint64(s.AtMS.ValueInt64())},
		}}, nil
	case "daily":
		return api.RoutineSchedule{Variant: api.RoutineScheduleDaily{
			Value: api.DailySchedule{Timezone: tz, Hour: hour, Minute: minute},
		}}, nil
	case "weekly":
		var days []string
		if diags := s.Weekdays.ElementsAs(ctx, &days, false); diags.HasError() {
			return api.RoutineSchedule{}, fmt.Errorf("weekdays is not a list of strings")
		}
		weekdays := make([]api.Weekday, 0, len(days))
		for _, d := range days {
			weekdays = append(weekdays, api.Weekday(d))
		}
		return api.RoutineSchedule{Variant: api.RoutineScheduleWeekly{
			Value: api.WeeklySchedule{Timezone: tz, Hour: hour, Minute: minute, Weekdays: weekdays},
		}}, nil
	case "monthly":
		return api.RoutineSchedule{Variant: api.RoutineScheduleMonthly{
			Value: api.MonthlySchedule{Timezone: tz, Hour: hour, Minute: minute, DayOfMonth: uint32(s.DayOfMonth.ValueInt64())},
		}}, nil
	default: // yearly
		return api.RoutineSchedule{Variant: api.RoutineScheduleYearly{
			Value: api.YearlySchedule{
				Timezone: tz, Hour: hour, Minute: minute,
				Month: uint32(s.Month.ValueInt64()), DayOfMonth: uint32(s.DayOfMonth.ValueInt64()),
			},
		}}, nil
	}
}

// fromSchedule flattens the union back, leaving every attribute the chosen
// variant does not use as null so it does not read as drift.
func fromSchedule(ctx context.Context, v api.RoutineSchedule) *scheduleModel {
	s := &scheduleModel{
		IntervalS:  types.Int64Null(),
		AtMS:       types.Int64Null(),
		Timezone:   types.StringNull(),
		Hour:       types.Int64Null(),
		Minute:     types.Int64Null(),
		Weekdays:   types.ListNull(types.StringType),
		DayOfMonth: types.Int64Null(),
		Month:      types.Int64Null(),
	}
	switch variant := v.Variant.(type) {
	case api.RoutineScheduleManual:
		s.Type = types.StringValue("manual")
	case api.RoutineScheduleEvery:
		s.Type = types.StringValue("every")
		s.IntervalS = types.Int64Value(int64(variant.Value.IntervalSecs))
	case api.RoutineScheduleOnce:
		s.Type = types.StringValue("once")
		s.AtMS = types.Int64Value(int64(variant.Value.AtMs))
	case api.RoutineScheduleDaily:
		s.Type = types.StringValue("daily")
		s.Timezone = types.StringValue(variant.Value.Timezone)
		s.Hour = types.Int64Value(int64(variant.Value.Hour))
		s.Minute = types.Int64Value(int64(variant.Value.Minute))
	case api.RoutineScheduleWeekly:
		s.Type = types.StringValue("weekly")
		s.Timezone = types.StringValue(variant.Value.Timezone)
		s.Hour = types.Int64Value(int64(variant.Value.Hour))
		s.Minute = types.Int64Value(int64(variant.Value.Minute))
		days := make([]string, 0, len(variant.Value.Weekdays))
		for _, d := range variant.Value.Weekdays {
			days = append(days, string(d))
		}
		s.Weekdays = listFromStrings(ctx, days)
	case api.RoutineScheduleMonthly:
		s.Type = types.StringValue("monthly")
		s.Timezone = types.StringValue(variant.Value.Timezone)
		s.Hour = types.Int64Value(int64(variant.Value.Hour))
		s.Minute = types.Int64Value(int64(variant.Value.Minute))
		s.DayOfMonth = types.Int64Value(int64(variant.Value.DayOfMonth))
	case api.RoutineScheduleYearly:
		s.Type = types.StringValue("yearly")
		s.Timezone = types.StringValue(variant.Value.Timezone)
		s.Hour = types.Int64Value(int64(variant.Value.Hour))
		s.Minute = types.Int64Value(int64(variant.Value.Minute))
		s.Month = types.Int64Value(int64(variant.Value.Month))
		s.DayOfMonth = types.Int64Value(int64(variant.Value.DayOfMonth))
	default:
		// A variant this provider predates. Naming it beats a silent "manual".
		s.Type = types.StringValue(fmt.Sprintf("unsupported(%T)", v.Variant))
	}
	return s
}

// toEnvironment turns the flattened block into the union, naming any attribute
// that does not belong to the chosen type.
func toEnvironment(s *environmentSpecModel) (api.EnvironmentSpec, error) {
	if s == nil {
		return api.EnvironmentSpec{}, fmt.Errorf(
			"an environment block is required: horsie has no default for where a run happens")
	}
	switch kind := strings.ToLower(s.Type.ValueString()); kind {
	case "runtime":
		if !s.Name.IsNull() {
			return api.EnvironmentSpec{}, fmt.Errorf("environment type \"runtime\" does not use: name")
		}
		if s.Vendor.IsNull() {
			return api.EnvironmentSpec{}, fmt.Errorf("environment type \"runtime\" needs: vendor")
		}
		env := api.RuntimeEnvironment{Vendor: s.Vendor.ValueString()}
		if len(s.Repos) > 0 {
			repos := make([]api.RepoConfig, 0, len(s.Repos))
			for _, rc := range s.Repos {
				one := api.RepoConfig{URL: rc.URL.ValueString()}
				if !rc.GitRef.IsNull() {
					v := rc.GitRef.ValueString()
					one.GitRef = &v
				}
				if !rc.Dir.IsNull() {
					v := rc.Dir.ValueString()
					one.Dir = &v
				}
				repos = append(repos, one)
			}
			env.Repos = &repos
		}
		return api.EnvironmentSpec{Variant: api.EnvironmentSpecRuntime{Value: env}}, nil
	case "named":
		var stray []string
		if !s.Vendor.IsNull() {
			stray = append(stray, "vendor")
		}
		if len(s.Repos) > 0 {
			stray = append(stray, "repos")
		}
		if len(stray) > 0 {
			return api.EnvironmentSpec{}, fmt.Errorf("environment type \"named\" does not use: %s", strings.Join(stray, ", "))
		}
		if s.Name.IsNull() {
			return api.EnvironmentSpec{}, fmt.Errorf("environment type \"named\" needs: name")
		}
		return api.EnvironmentSpec{Variant: api.EnvironmentSpecNamed{
			Value: api.NamedEnvironment{Name: s.Name.ValueString()},
		}}, nil
	default:
		return api.EnvironmentSpec{}, fmt.Errorf(
			"unknown environment type %q (expected runtime or named)", s.Type.ValueString())
	}
}

// fromEnvironment flattens the union back, leaving what the variant does not
// use as null so it does not read as drift.
func fromEnvironment(v api.EnvironmentSpec) *environmentSpecModel {
	e := &environmentSpecModel{Vendor: types.StringNull(), Name: types.StringNull()}
	switch variant := v.Variant.(type) {
	case api.EnvironmentSpecRuntime:
		e.Type = types.StringValue("runtime")
		e.Vendor = types.StringValue(variant.Value.Vendor)
		if variant.Value.Repos != nil {
			for _, rc := range *variant.Value.Repos {
				e.Repos = append(e.Repos, repoModel{
					URL:    types.StringValue(rc.URL),
					GitRef: optString(rc.GitRef),
					Dir:    optString(rc.Dir),
				})
			}
		}
	case api.EnvironmentSpecNamed:
		e.Type = types.StringValue("named")
		e.Name = types.StringValue(variant.Value.Name)
	default:
		e.Type = types.StringValue(fmt.Sprintf("unsupported(%T)", v.Variant))
	}
	return e
}

func (r *routineResource) input(ctx context.Context, m routineModel) (api.RoutineInput, error) {
	in := api.RoutineInput{
		Name:   m.Name.ValueString(),
		Agent:  m.Agent.ValueString(),
		Prompt: m.Prompt.ValueString(),
	}
	if !m.Description.IsNull() && !m.Description.IsUnknown() {
		v := m.Description.ValueString()
		in.Description = &v
	}
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		v := m.Enabled.ValueBool()
		in.Enabled = &v
	}
	env, err := toEnvironment(m.Environment)
	if err != nil {
		return in, err
	}
	in.Environment = env
	sched, err := toSchedule(ctx, m.Schedule)
	if err != nil {
		return in, err
	}
	in.Schedule = &sched
	return in, nil
}

func applyRoutine(ctx context.Context, m *routineModel, v *api.RoutineView) {
	m.Name = types.StringValue(v.Name)
	m.Description = types.StringValue(v.Description)
	m.Agent = types.StringValue(v.Agent)
	m.Prompt = types.StringValue(v.Prompt)
	m.Enabled = types.BoolValue(v.Enabled)
	m.Schedule = fromSchedule(ctx, v.Schedule)
	m.Environment = fromEnvironment(v.Environment)
	if v.NextRunAtMs != nil {
		m.NextRunAtMS = types.Int64Value(int64(*v.NextRunAtMs))
	} else {
		m.NextRunAtMS = types.Int64Null()
	}
}

func (r *routineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan routineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, err := r.input(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid schedule", err.Error())
		return
	}
	view, err := r.client.CreateRoutine(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Could not create routine", err.Error())
		return
	}
	applyRoutine(ctx, &plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *routineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state routineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetRoutine(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read routine", err.Error())
		return
	}
	applyRoutine(ctx, &state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *routineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan routineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, err := r.input(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid schedule", err.Error())
		return
	}
	view, err := r.client.ReplaceRoutine(ctx, plan.Name.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Could not update routine", err.Error())
		return
	}
	applyRoutine(ctx, &plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *routineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state routineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteRoutine(ctx, state.Name.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Could not delete routine", err.Error())
	}
}

func (r *routineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
