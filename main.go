// terraform-provider-horsie manages a horsie server's configuration as code.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/blossomstack/terraform-provider-horsie/internal/provider"
)

// Set by the release build from the git tag.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/blossomstack/horsie",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
