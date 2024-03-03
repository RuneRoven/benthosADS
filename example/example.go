package main

import (
	"context"

	"github.com/benthosdev/benthos/v4/public/service"

	// Import all standard Benthos components
	_ "github.com/benthosdev/benthos/v4/public/components/all"

	_ "github.com/RuneRoven/benthosADS"
)
 
func main() {
	service.RunCLI(context.Background())
}
  