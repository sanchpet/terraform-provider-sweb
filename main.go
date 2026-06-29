// Command terraform-provider-sweb is the Terraform provider for the SpaceWeb
// (sweb.ru) hosting API, built on github.com/sanchpet/sweb-go-sdk.
package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/sanchpet/terraform-provider-sweb/internal/provider"
)

// version is set at release time via -ldflags by GoReleaser.
var version = "dev"

func main() {
	if err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/sanchpet/sweb",
	}); err != nil {
		log.Fatal(err)
	}
}
