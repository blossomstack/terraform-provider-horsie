package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// A stand-in horsie that records what it was asked and answers from a map.
func fake(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL, "test-token")
}

func TestPutModelProviderSendsTokenAndDecodesView(t *testing.T) {
	var gotAuth, gotPath, gotMethod string
	var gotBody api.ProviderInput
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotMethod = r.Header.Get("Authorization"), r.URL.Path, r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		base := "https://api.example.com"
		_ = json.NewEncoder(w).Encode(api.ProviderView{
			Name: "p", Kind: "anthropic", BaseURL: &base, HasCredential: true,
		})
	})

	key := "sk-secret"
	view, err := c.PutModelProvider(context.Background(), "p", api.ProviderInput{
		Name: "p", Kind: "anthropic", APIKey: &key,
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/config/model-providers/p" {
		t.Errorf("%s %s, want PUT /api/config/model-providers/p", gotMethod, gotPath)
	}
	if gotBody.APIKey == nil || *gotBody.APIKey != "sk-secret" {
		t.Errorf("apiKey did not survive the wire: %+v", gotBody.APIKey)
	}
	if !view.HasCredential {
		t.Error("hasCredential should decode as true")
	}
}

// The name is camelCased on the wire by fluorite; a snake_case key would be
// silently ignored, so this pins the encoding rather than trusting it.
func TestProviderInputEncodesCamelCase(t *testing.T) {
	base := "http://localhost:1"
	yes := true
	body, err := json.Marshal(api.ProviderInput{
		Name: "p", Kind: "anthropic", BaseURL: &base, KeepThinkingSignature: &yes,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"baseUrl", "keepThinkingSignature"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing %q in %s", key, body)
		}
	}
}

// An omitted api_key must not appear at all: horsie reads "absent" as "keep the
// stored key", and sending an explicit null would be a different instruction.
func TestOmittedAPIKeyIsAbsentNotNull(t *testing.T) {
	body, err := json.Marshal(api.ProviderInput{Name: "p", Kind: "anthropic"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := raw["apiKey"]; present {
		t.Errorf("apiKey must be omitted entirely, got %s", body)
	}
}

func TestNotFoundIsDetectable(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	err := c.DeleteModel(context.Background(), "ghost")
	if !IsNotFound(err) {
		t.Fatalf("want a detectable 404, got %v", err)
	}
}

func TestGetModelProviderFiltersTheList(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.ProviderView{
			{Name: "a", Kind: "openai"},
			{Name: "b", Kind: "anthropic"},
		})
	})
	got, err := c.GetModelProvider(context.Background(), "b")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Kind != "anthropic" {
		t.Errorf("picked the wrong provider: %+v", got)
	}

	if _, err := c.GetModelProvider(context.Background(), "missing"); !IsNotFound(err) {
		t.Errorf("an absent provider should read as 404, got %v", err)
	}
}

// A 409 carries meaning the provider surfaces to the operator: the provider is
// held open by a model.
func TestConflictKeepsItsStatusAndBody(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"provider 'p' is still used by model(s): sonnet"}`))
	})
	err := c.DeleteModelProvider(context.Background(), "p")
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("want *Error, got %T", err)
	}
	if apiErr.Status != http.StatusConflict {
		t.Errorf("status = %d, want 409", apiErr.Status)
	}
	if apiErr.Body == "" {
		t.Error("the server's explanation should survive into the diagnostic")
	}
}
