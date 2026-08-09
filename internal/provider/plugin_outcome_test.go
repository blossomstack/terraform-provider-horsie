package provider

import (
	"strings"
	"testing"

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
