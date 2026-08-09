package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// Every kind must reach the union it names. This is the whole risk of
// flattening a sum type into a block of optional attributes.
func TestMcpAuthReachesEveryVariant(t *testing.T) {
	cases := []struct {
		name string
		in   mcpAuthModel
		want any
	}{
		{"none", mcpAuthModel{Kind: types.StringValue("none")}, api.McpAuthInputNone{}},
		{"github_app", mcpAuthModel{Kind: types.StringValue("github_app")}, api.McpAuthInputGithubApp{}},
		{"bearer", mcpAuthModel{
			Kind: types.StringValue("bearer"), Token: types.StringValue("t"),
		}, api.McpAuthInputBearer{}},
		{"bearer without a token", mcpAuthModel{
			Kind: types.StringValue("bearer"),
		}, api.McpAuthInputBearer{}},
		{"oauth", mcpAuthModel{
			Kind:     types.StringValue("oauth"),
			ClientID: types.StringValue("abc"), ClientSecret: types.StringValue("s"),
			TokenEndpoint: types.StringValue("https://auth.example.com/token"),
		}, api.McpAuthInputOAuth{}},
		{"oauth with nothing set", mcpAuthModel{
			Kind: types.StringValue("oauth"),
		}, api.McpAuthInputOAuth{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toMcpAuth(&tc.in)
			if err != nil {
				t.Fatalf("toMcpAuth: %v", err)
			}
			if got, want := fmt.Sprintf("%T", got.Variant), fmt.Sprintf("%T", tc.want); got != want {
				t.Errorf("variant = %s, want %s", got, want)
			}
		})
	}
}

// An attribute the chosen kind does not use is an error, not something quietly
// dropped on the way to the wire.
func TestMcpAuthRejectsStrayAttributes(t *testing.T) {
	cases := []struct {
		name string
		in   mcpAuthModel
		want string
	}{
		{"token on none", mcpAuthModel{
			Kind: types.StringValue("none"), Token: types.StringValue("t"),
		}, "token"},
		{"client_id on bearer", mcpAuthModel{
			Kind: types.StringValue("bearer"), ClientID: types.StringValue("abc"),
		}, "client_id"},
		{"token on oauth", mcpAuthModel{
			Kind: types.StringValue("oauth"), Token: types.StringValue("t"),
		}, "token"},
		{"token on github_app", mcpAuthModel{
			Kind: types.StringValue("github_app"), Token: types.StringValue("t"),
		}, "token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := toMcpAuth(&tc.in)
			if err == nil {
				t.Fatal("want an error naming the stray attribute")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name %q", err, tc.want)
			}
		})
	}
}

func TestMcpAuthRejectsUnknownKind(t *testing.T) {
	_, err := toMcpAuth(&mcpAuthModel{Kind: types.StringValue("basic")})
	if err == nil || !strings.Contains(err.Error(), "basic") {
		t.Fatalf("err = %v, want it to name the bad kind", err)
	}
}

func TestMcpAuthRequiresTheBlock(t *testing.T) {
	_, err := toMcpAuth(nil)
	if err == nil || !strings.Contains(err.Error(), "none") {
		t.Fatalf("err = %v, want it to point at kind = \"none\"", err)
	}
}

// Null means "not managed here", which is horsie's "omitted keeps the stored
// secret". An empty string is a deliberate clear and must reach the wire.
func TestBearerTokenFollowsOmitKeepClear(t *testing.T) {
	kept, err := toMcpAuth(&mcpAuthModel{Kind: types.StringValue("bearer")})
	if err != nil {
		t.Fatalf("toMcpAuth: %v", err)
	}
	if got := kept.Variant.(api.McpAuthInputBearer).Value.Token; got != nil {
		t.Errorf("an omitted token must be absent, got %q", *got)
	}

	cleared, err := toMcpAuth(&mcpAuthModel{
		Kind: types.StringValue("bearer"), Token: types.StringValue(""),
	})
	if err != nil {
		t.Fatalf("toMcpAuth: %v", err)
	}
	got := cleared.Variant.(api.McpAuthInputBearer).Value.Token
	if got == nil || *got != "" {
		t.Errorf("an empty token must be sent as \"\" to clear, got %v", got)
	}
}

// The view never carries a secret, so folding it back must not erase what the
// configuration said.
func TestApplyMcpAuthKeepsConfiguredSecrets(t *testing.T) {
	a := &mcpAuthModel{
		Kind:                  types.StringValue("oauth"),
		ClientSecret:          types.StringValue("s3cret"),
		AuthorizationEndpoint: types.StringValue("https://auth.example.com/authorize"),
	}
	applyMcpAuth(a, api.McpAuthView{Variant: api.McpAuthViewOAuth{Value: api.McpOAuthView{
		Connected: true, ClientID: strPtr("issued-id"), HasClientSecret: true,
	}}})

	if a.ClientSecret.ValueString() != "s3cret" {
		t.Error("client_secret was erased by a refresh")
	}
	if a.AuthorizationEndpoint.ValueString() != "https://auth.example.com/authorize" {
		t.Error("authorization_endpoint was erased by a refresh: the view does not carry it")
	}
	if a.ClientID.ValueString() != "issued-id" {
		t.Errorf("client_id = %q, want the dynamically registered id", a.ClientID.ValueString())
	}
	if !a.Connected.ValueBool() || !a.HasClientSecret.ValueBool() {
		t.Error("the redacted flags should come from the view")
	}
}

// Every computed flag must be known after apply, whatever the kind — an unknown
// one fails the apply with "Provider produced inconsistent result".
func TestApplyMcpAuthAlwaysSetsEveryFlag(t *testing.T) {
	views := []api.McpAuthView{
		{Variant: api.McpAuthViewNone{}},
		{Variant: api.McpAuthViewGithubApp{}},
		{Variant: api.McpAuthViewBearer{Value: api.McpBearerView{HasToken: true}}},
		{Variant: api.McpAuthViewOAuth{Value: api.McpOAuthView{}}},
	}
	for _, v := range views {
		a := &mcpAuthModel{}
		applyMcpAuth(a, v)
		if a.HasToken.IsNull() || a.HasClientSecret.IsNull() || a.Connected.IsNull() {
			t.Errorf("%T left a computed flag null", v.Variant)
		}
		if a.Kind.IsNull() {
			t.Errorf("%T left kind null", v.Variant)
		}
	}
}

func strPtr(s string) *string { return &s }
