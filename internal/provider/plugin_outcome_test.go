package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

func TestInstalledUnwrapsABundle(t *testing.T) {
	view, err := installed(&api.InstallOutcome{
		Variant: api.InstallOutcomeInstalled{Value: api.PluginView{Name: "toolkit"}},
	})
	if err != nil {
		t.Fatalf("installed: %v", err)
	}
	if view.Name != "toolkit" {
		t.Errorf("name = %q, want toolkit", view.Name)
	}
}

// Both outcomes are a 201 with a row created, so a marketplace URL would
// otherwise be an apply that succeeded and installed nothing.
func TestInstalledRejectsAMarketplace(t *testing.T) {
	_, err := installed(&api.InstallOutcome{
		Variant: api.InstallOutcomeMarketplace{Value: api.MarketplaceView{
			Name:        "acme",
			SourceURL:   "https://example.com/acme.git",
			PluginCount: 2,
			Plugins: []api.MarketplacePluginView{
				{Name: "reviewer"}, {Name: "releaser"},
			},
		}},
	})
	if err == nil {
		t.Fatal("a marketplace must be an error, not a silent no-op")
	}
	for _, want := range []string{"marketplace", "reviewer", "releaser", "https://example.com/acme.git"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// catalogEntryType is hand-written, so it can drift from the schema it is
// supposed to mirror. That drift is invisible until an apply fails with a
// value-conversion error, so pin the two together here.
func TestCatalogElementTypeMatchesTheSchema(t *testing.T) {
	var resp resource.SchemaResponse
	NewPluginResource().(*pluginResource).Schema(context.Background(), resource.SchemaRequest{}, &resp)

	attribute, ok := resp.Schema.Attributes["catalog"].(schema.ListNestedAttribute)
	if !ok {
		t.Fatalf("catalog is %T, want a ListNestedAttribute", resp.Schema.Attributes["catalog"])
	}
	want := attribute.NestedObject.Type()
	if got := catalogEntryType; !got.Equal(want) {
		t.Errorf("catalogEntryType = %v, want %v", got, want)
	}
}
