package github.com/RuneRoven/benthosADS/example

import (
	"context"

	"github.com/benthosdev/benthos/v4/public/service"

	// Import all standard Benthos components
	_ "github.com/benthosdev/benthos/v4/public/components/all"

	"github.com/RuneRoven/benthosADS"
)
 
func main() {
	service.RunCLI(context.Background())
}
  