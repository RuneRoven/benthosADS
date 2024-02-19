package main

import (
	"github.com/benthosdev/benthos/v4/public/service"

	// Import all standard Benthos components
	_ "github.com/benthosdev/benthos/v4/public/components/all"



)

func main() {
	service.RunCLI(context.Background())
}