// Package setup wires the parts of o setup into the engine in run order.
package setup

import (
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/envfiles"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/handbook"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/identity"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/prepare"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/repos"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/root"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/services"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/system"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/toolchains"
)

// Default returns every setup part in run order. Each part's Needs appear before it.
func Default() []engine.Part {
	return []engine.Part{
		system.New(),
		identity.New(),
		handbook.New(),
		repos.New(),
		root.New(),
		toolchains.New(),
		services.New(),
		envfiles.New(),
		prepare.New(),
	}
}
