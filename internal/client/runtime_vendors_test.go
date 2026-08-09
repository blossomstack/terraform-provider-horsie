package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// A settings union with no variant set cannot even be encoded, so every
// fixture here carries one. That is the same failure a resource with no `fly`
// or `velos` block would hit mid-apply, which is why the resource refuses it at
// validate time instead.
func flySettings() api.RuntimeVendorSettings {
	return api.RuntimeVendorSettings{Variant: api.RuntimeVendorSettingsFly{
		Value: api.FlyVendorSettings{App: "horsie-runtimes", Image: "ghcr.io/x/runtime:1"},
	}}
}

// horsie offers no per-name read, so GetRuntimeVendor filters the list. The
// 404 it synthesizes is what Read turns into "removed outside Terraform", so it
// has to be a *client.Error and not a bare error.
func TestGetRuntimeVendorSynthesizesNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runtime-vendors" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]api.RuntimeVendorConfigView{{Name: "other", Settings: flySettings()}})
	}))
	defer srv.Close()

	if _, err := New(srv.URL, "t").GetRuntimeVendor(context.Background(), "missing"); !IsNotFound(err) {
		t.Fatalf("want a 404, got %v", err)
	}
}

func TestGetRuntimeVendorFindsTheNamedOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.RuntimeVendorConfigView{
			{Name: "other", Settings: flySettings()},
			{Name: "fly-prod", Settings: flySettings(), HasCredential: true},
		})
	}))
	defer srv.Close()

	got, err := New(srv.URL, "t").GetRuntimeVendor(context.Background(), "fly-prod")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "fly-prod" || !got.HasCredential {
		t.Errorf("got %+v", got)
	}
}

// A vendor name is free text, so it is escaped into the path rather than
// concatenated — a name with a slash would otherwise address a route that does
// not exist.
func TestPutRuntimeVendorEscapesTheNameIntoThePath(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.EscapedPath(), r.Method
		_ = json.NewEncoder(w).Encode(api.RuntimeVendorConfigView{Name: "fly prod", Settings: flySettings()})
	}))
	defer srv.Close()

	_, err := New(srv.URL, "t").PutRuntimeVendor(context.Background(), "fly prod",
		api.RuntimeVendorConfigInput{Name: "fly prod", Settings: flySettings()})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/api/runtime-vendors/fly%20prod" {
		t.Errorf("path = %q, want the name escaped into it", gotPath)
	}
}

func TestDeleteRuntimeVendorTolerates204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := New(srv.URL, "t").DeleteRuntimeVendor(context.Background(), "fly-prod"); err != nil {
		t.Fatal(err)
	}
}

func TestSetDefaultRuntimeVendorHitsTheRenamedRoute(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody api.DefaultRuntimeVendorInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(api.SettingsView{DefaultRuntimeVendor: "fly-prod"})
	}))
	defer srv.Close()

	got, err := New(srv.URL, "t").SetDefaultRuntimeVendor(context.Background(), "fly-prod")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/config/default-runtime-vendor" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
	if gotBody.Vendor != "fly-prod" {
		t.Errorf("body vendor = %q", gotBody.Vendor)
	}
	if got.DefaultRuntimeVendor != "fly-prod" {
		t.Errorf("default = %q", got.DefaultRuntimeVendor)
	}
}

// Clearing is a DELETE, not a PUT of "": horsie refuses an empty vendor, and
// only the DELETE falls back to the built-in `local`.
func TestClearDefaultRuntimeVendorDeletes(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_ = json.NewEncoder(w).Encode(api.SettingsView{DefaultRuntimeVendor: "local"})
	}))
	defer srv.Close()

	got, err := New(srv.URL, "t").ClearDefaultRuntimeVendor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if got.DefaultRuntimeVendor != "local" {
		t.Errorf("default = %q", got.DefaultRuntimeVendor)
	}
}
