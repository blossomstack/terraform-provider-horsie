package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/blossomstack/terraform-provider-horsie/internal/client"
	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

var (
	_ resource.Resource                = (*mcpServerResource)(nil)
	_ resource.ResourceWithConfigure   = (*mcpServerResource)(nil)
	_ resource.ResourceWithImportState = (*mcpServerResource)(nil)
)

type mcpServerResource struct{ client *client.Client }

// NewMcpServerResource registers `horsie_mcp_server`.
func NewMcpServerResource() resource.Resource { return &mcpServerResource{} }

// mcpAuthModel is HCL's flattening of horsie's `McpAuthInput` union: a
// discriminator plus the fields each variant needs, validated here rather than
// arriving as a 422 halfway through an apply.
//
// The three `has_*`/`connected` flags are the redacted half of the matching
// view — horsie never returns a secret, only whether one is stored.
type mcpAuthModel struct {
	Kind                  types.String `tfsdk:"kind"`
	Token                 types.String `tfsdk:"token"`
	ClientID              types.String `tfsdk:"client_id"`
	ClientSecret          types.String `tfsdk:"client_secret"`
	AuthorizationEndpoint types.String `tfsdk:"authorization_endpoint"`
	TokenEndpoint         types.String `tfsdk:"token_endpoint"`
	RegistrationEndpoint  types.String `tfsdk:"registration_endpoint"`
	HasToken              types.Bool   `tfsdk:"has_token"`
	HasClientSecret       types.Bool   `tfsdk:"has_client_secret"`
	Connected             types.Bool   `tfsdk:"connected"`
}

type mcpServerModel struct {
	Name      types.String  `tfsdk:"name"`
	URL       types.String  `tfsdk:"url"`
	Auth      *mcpAuthModel `tfsdk:"auth"`
	Enabled   types.Bool    `tfsdk:"enabled"`
	ToolCount types.Int64   `tfsdk:"tool_count"`
	LastError types.String  `tfsdk:"last_error"`
}

func (r *mcpServerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mcp_server"
}

func (r *mcpServerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A remote MCP server an agent preset can be granted.\n\n" +
			"Its tools are namespaced `mcp__<name>__<tool>`, so the name is part of what a preset " +
			"refers to and not just a label.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Stable id, and the prefix its tools are namespaced under. " +
					"Changing it replaces the resource: horsie has no rename, and a silent move would " +
					"strand every preset that granted the old name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"url": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Streamable-HTTP endpoint.",
			},
			"enabled": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether the last connect succeeded and the server is usable. " +
					"Set by horsie, not here — a server is enabled by working, not by being asked to.",
			},
			"tool_count": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Tools advertised at the last successful connect.",
			},
			"last_error": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Why the last connect failed, when it did.",
			},
		},
		Blocks: map[string]schema.Block{
			"auth": schema.SingleNestedBlock{
				MarkdownDescription: "How horsie authenticates to the server. **Required** — say `kind = \"none\"` " +
					"for a public server rather than leaving it out, so an unauthenticated call is a decision " +
					"in the configuration rather than an omission.",
				Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{
						Required: true,
						MarkdownDescription: "One of `none`, `bearer`, `github_app` or `oauth`. Each kind reads only " +
							"the attributes it needs; setting one it does not use is an error rather than being " +
							"silently ignored.",
					},
					"token": schema.StringAttribute{
						Optional:  true,
						Sensitive: true,
						MarkdownDescription: "`bearer`: the static token. Omit to leave a stored token untouched, " +
							"or set `\"\"` to clear it.",
					},
					"client_id": schema.StringAttribute{
						Optional: true,
						Computed: true,
						MarkdownDescription: "`oauth`: a pre-registered client id. Omit to let horsie register one " +
							"dynamically (RFC 7591), in which case the id it was issued appears here.",
					},
					"client_secret": schema.StringAttribute{
						Optional:            true,
						Sensitive:           true,
						MarkdownDescription: "`oauth`: the client secret. Omit to keep, `\"\"` to clear.",
					},
					"authorization_endpoint": schema.StringAttribute{
						Optional: true,
						MarkdownDescription: "`oauth`: override discovery when the server publishes no metadata " +
							"(RFC 9728 / 8414).",
					},
					"token_endpoint": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "`oauth`: as `authorization_endpoint`.",
					},
					"registration_endpoint": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "`oauth`: as `authorization_endpoint`.",
					},
					"has_token": schema.BoolAttribute{
						Computed:            true,
						MarkdownDescription: "Whether a bearer token is stored. The server never returns the token itself.",
					},
					"has_client_secret": schema.BoolAttribute{
						Computed:            true,
						MarkdownDescription: "Whether an OAuth client secret is stored.",
					},
					"connected": schema.BoolAttribute{
						Computed: true,
						MarkdownDescription: "`oauth`: whether an access token has been obtained. Terraform can write " +
							"the client configuration but cannot complete the sign-in, which is a browser redirect — " +
							"so a freshly applied `oauth` server is `connected = false` until someone authorises it " +
							"from horsie's settings page.",
					},
				},
			},
		},
	}
}

func (r *mcpServerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// optSecret maps a configured secret onto horsie's omit=keep, ""=clear
// convention: null means "not managed here" and is left out entirely, while an
// empty string is a deliberate clear and is sent.
func optSecret(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

// toMcpAuth turns the flattened block into the union, naming any attribute that
// does not belong to the chosen kind.
func toMcpAuth(a *mcpAuthModel) (api.McpAuthInput, error) {
	if a == nil {
		return api.McpAuthInput{}, fmt.Errorf(
			"an auth block is required: say kind = \"none\" for a server that needs no credentials")
	}

	set := map[string]bool{
		"token":                  !a.Token.IsNull(),
		"client_id":              !a.ClientID.IsNull() && !a.ClientID.IsUnknown(),
		"client_secret":          !a.ClientSecret.IsNull(),
		"authorization_endpoint": !a.AuthorizationEndpoint.IsNull(),
		"token_endpoint":         !a.TokenEndpoint.IsNull(),
		"registration_endpoint":  !a.RegistrationEndpoint.IsNull(),
	}
	allowed := map[string][]string{
		"none":       {},
		"bearer":     {"token"},
		"github_app": {},
		"oauth": {
			"client_id", "client_secret",
			"authorization_endpoint", "token_endpoint", "registration_endpoint",
		},
	}
	kind := strings.ToLower(a.Kind.ValueString())
	permitted, ok := allowed[kind]
	if !ok {
		return api.McpAuthInput{}, fmt.Errorf(
			"unknown auth kind %q (expected none, bearer, github_app or oauth)", a.Kind.ValueString())
	}
	usable := map[string]bool{}
	for _, name := range permitted {
		usable[name] = true
	}
	var stray []string
	for _, name := range []string{
		"token", "client_id", "client_secret",
		"authorization_endpoint", "token_endpoint", "registration_endpoint",
	} {
		if set[name] && !usable[name] {
			stray = append(stray, name)
		}
	}
	if len(stray) > 0 {
		return api.McpAuthInput{}, fmt.Errorf("auth kind %q does not use: %s", kind, strings.Join(stray, ", "))
	}

	switch kind {
	case "none":
		return api.McpAuthInput{Variant: api.McpAuthInputNone{Value: api.McpNoAuth{}}}, nil
	case "github_app":
		return api.McpAuthInput{Variant: api.McpAuthInputGithubApp{Value: api.McpGithubAppAuth{}}}, nil
	case "bearer":
		return api.McpAuthInput{Variant: api.McpAuthInputBearer{
			Value: api.McpBearerInput{Token: optSecret(a.Token)},
		}}, nil
	default: // oauth
		return api.McpAuthInput{Variant: api.McpAuthInputOAuth{Value: api.McpOAuthInput{
			ClientID:              optSecret(a.ClientID),
			ClientSecret:          optSecret(a.ClientSecret),
			AuthorizationEndpoint: optSecret(a.AuthorizationEndpoint),
			TokenEndpoint:         optSecret(a.TokenEndpoint),
			RegistrationEndpoint:  optSecret(a.RegistrationEndpoint),
		}}}, nil
	}
}

// applyMcpAuth folds the redacted view back into the configured block.
//
// It writes only what horsie actually returns. The token, the client secret and
// the three endpoint overrides are never echoed, so overwriting them would
// erase the configuration from state on the first refresh; they stay as
// configured, exactly as `horsie_model_provider` treats `api_key`.
func applyMcpAuth(a *mcpAuthModel, v api.McpAuthView) {
	a.HasToken = types.BoolValue(false)
	a.HasClientSecret = types.BoolValue(false)
	a.Connected = types.BoolValue(false)

	switch variant := v.Variant.(type) {
	case api.McpAuthViewNone:
		a.Kind = types.StringValue("none")
		a.ClientID = types.StringNull()
	case api.McpAuthViewGithubApp:
		a.Kind = types.StringValue("github_app")
		a.ClientID = types.StringNull()
	case api.McpAuthViewBearer:
		a.Kind = types.StringValue("bearer")
		a.ClientID = types.StringNull()
		a.HasToken = types.BoolValue(variant.Value.HasToken)
	case api.McpAuthViewOAuth:
		a.Kind = types.StringValue("oauth")
		a.ClientID = optString(variant.Value.ClientID)
		a.HasClientSecret = types.BoolValue(variant.Value.HasClientSecret)
		a.Connected = types.BoolValue(variant.Value.Connected)
	default:
		// A variant this provider predates. Naming it beats guessing.
		a.Kind = types.StringValue(fmt.Sprintf("unsupported(%T)", v.Variant))
		a.ClientID = types.StringNull()
	}
}

func applyMcpServer(m *mcpServerModel, v *api.McpServerView) {
	m.Name = types.StringValue(v.Name)
	m.URL = types.StringValue(v.URL)
	m.Enabled = types.BoolValue(v.Enabled)
	if v.ToolCount != nil {
		m.ToolCount = types.Int64Value(int64(*v.ToolCount))
	} else {
		m.ToolCount = types.Int64Null()
	}
	m.LastError = optString(v.LastError)
	if m.Auth == nil {
		m.Auth = &mcpAuthModel{}
	}
	applyMcpAuth(m.Auth, v.Auth)
}

func (r *mcpServerResource) write(ctx context.Context, plan *mcpServerModel) error {
	auth, err := toMcpAuth(plan.Auth)
	if err != nil {
		return err
	}
	view, err := r.client.PutMcpServer(ctx, plan.Name.ValueString(), api.McpServerInput{
		Name: plan.Name.ValueString(),
		URL:  plan.URL.ValueString(),
		Auth: auth,
	})
	if err != nil {
		return err
	}
	applyMcpServer(plan, view)
	return nil
}

func (r *mcpServerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mcpServerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Could not create MCP server", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *mcpServerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mcpServerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetMcpServer(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read MCP server", err.Error())
		return
	}
	applyMcpServer(&state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *mcpServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan mcpServerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Could not update MCP server", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *mcpServerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mcpServerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteMcpServer(ctx, state.Name.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Could not delete MCP server", err.Error())
	}
}

func (r *mcpServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
