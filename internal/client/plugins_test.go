package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

func TestInstallPluginSendsSourceAndDecodesInstalled(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody api.PluginInstallInput
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(api.InstallOutcome{
			Variant: api.InstallOutcomeInstalled{Value: api.PluginView{
				Name:      "toolkit",
				SourceURL: "https://example.com/toolkit.git",
				Catalog:   []api.CatalogEntryView{{Kind: "skill", Name: "review"}},
			}},
		})
	})

	url, ref := "https://example.com/toolkit.git", "9f3c1ab"
	out, err := c.InstallPlugin(context.Background(), api.PluginInstallInput{SourceURL: &url, SourceRef: &ref})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/plugins" {
		t.Errorf("%s %s, want POST /api/plugins", gotMethod, gotPath)
	}
	if gotBody.SourceRef == nil || *gotBody.SourceRef != "9f3c1ab" {
		t.Errorf("sourceRef did not survive the wire: %+v", gotBody.SourceRef)
	}
	if gotBody.Marketplace != nil || gotBody.PluginName != nil {
		t.Error("a plain-git install must not send marketplace fields: horsie rejects both together")
	}
	installed, ok := out.Variant.(api.InstallOutcomeInstalled)
	if !ok {
		t.Fatalf("outcome = %T, want Installed", out.Variant)
	}
	if installed.Value.Name != "toolkit" {
		t.Errorf("name = %q, want toolkit", installed.Value.Name)
	}
}

// The install body is camelCased by fluorite; a snake_case key would be
// silently ignored rather than rejected, so pin the encoding.
func TestPluginInstallInputEncodesCamelCase(t *testing.T) {
	url, ref := "https://example.com/t.git", "main"
	body, err := json.Marshal(api.PluginInstallInput{SourceURL: &url, SourceRef: &ref})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"sourceUrl", "sourceRef"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing %q in %s", key, body)
		}
	}
	if _, ok := raw["marketplace"]; ok {
		t.Errorf("an omitted marketplace must be absent, not null: %s", body)
	}
}

// GetPlugin filters a list rather than fetching one, so the "gone" case has to
// be synthesised — and Read depends on it being a 404.
func TestGetPluginMissingIsNotFound(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.PluginView{{Name: "other"}})
	})
	_, err := c.GetPlugin(context.Background(), "toolkit")
	if !IsNotFound(err) {
		t.Fatalf("err = %v, want a 404 so Read drops the resource from state", err)
	}
}

func TestGetPluginFindsByName(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.PluginView{
			{Name: "other"},
			{Name: "toolkit", SourceURL: "https://example.com/toolkit.git", EnabledDefault: true},
		})
	})
	view, err := c.GetPlugin(context.Background(), "toolkit")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.SourceURL != "https://example.com/toolkit.git" || !view.EnabledDefault {
		t.Errorf("got %+v, want the toolkit entry", view)
	}
}

func TestSetPluginDefaultPutsByName(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody api.PluginDefaultInput
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(api.PluginView{Name: "my toolkit", EnabledDefault: true})
	})

	if _, err := c.SetPluginDefault(context.Background(), "my toolkit", true); err != nil {
		t.Fatalf("set default: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/plugins/my toolkit" {
		t.Errorf("%s %s, want PUT /api/plugins/my toolkit", gotMethod, gotPath)
	}
	if !gotBody.EnabledDefault {
		t.Error("enabledDefault did not survive the wire")
	}
}
