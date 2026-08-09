package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

func TestPutMcpServerSendsTaggedAuth(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(api.McpServerView{
			Name: "linear", URL: "https://mcp.linear.app/mcp", Enabled: true,
			Auth: api.McpAuthView{Variant: api.McpAuthViewBearer{Value: api.McpBearerView{HasToken: true}}},
		})
	})

	token := "sk-linear"
	view, err := c.PutMcpServer(context.Background(), "linear", api.McpServerInput{
		Name: "linear", URL: "https://mcp.linear.app/mcp",
		Auth: api.McpAuthInput{Variant: api.McpAuthInputBearer{Value: api.McpBearerInput{Token: &token}}},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/mcp/servers/linear" {
		t.Errorf("%s %s, want PUT /api/mcp/servers/linear", gotMethod, gotPath)
	}

	// The union is adjacently tagged: a bare {"token": ...} would be rejected,
	// and a wrong tag would be a 422 rather than anything the types catch.
	auth, ok := gotBody["auth"].(map[string]any)
	if !ok {
		t.Fatalf("auth is not an object: %v", gotBody["auth"])
	}
	if auth["kind"] != "Bearer" {
		t.Errorf("auth.kind = %v, want Bearer", auth["kind"])
	}
	value, ok := auth["value"].(map[string]any)
	if !ok || value["token"] != "sk-linear" {
		t.Errorf("auth.value = %v, want the token under value", auth["value"])
	}

	bearer, ok := view.Auth.Variant.(api.McpAuthViewBearer)
	if !ok || !bearer.Value.HasToken {
		t.Errorf("view auth = %#v, want a bearer with a stored token", view.Auth.Variant)
	}
}

func TestGetMcpServerMissingIsNotFound(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(api.McpServerList{Servers: []api.McpServerView{{
			Name: "other", Auth: api.McpAuthView{Variant: api.McpAuthViewNone{}},
		}}})
	})
	if _, err := c.GetMcpServer(context.Background(), "linear"); !IsNotFound(err) {
		t.Fatalf("err = %v, want a 404 so Read drops the resource from state", err)
	}
}

func TestListMcpServersUnwrapsTheEnvelope(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(api.McpServerList{Servers: []api.McpServerView{
			{Name: "a", Auth: api.McpAuthView{Variant: api.McpAuthViewNone{}}},
			{Name: "b", Auth: api.McpAuthView{Variant: api.McpAuthViewGithubApp{}}},
		}})
	})
	got, err := c.ListMcpServers(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers, want 2 — the list arrives wrapped in {servers: ...}", len(got))
	}
}
