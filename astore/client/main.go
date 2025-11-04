package main

import (
	acommands "github.com/ccontavalli/enkit/astore/client/commands"
	bcommands "github.com/ccontavalli/enkit/lib/client/commands"

	"github.com/ccontavalli/enkit/lib/client"
	"github.com/ccontavalli/enkit/lib/kflags/kcobra"

	"github.com/ccontavalli/enkit/lib/srand"
	"math/rand"
)

func main() {
	base := client.DefaultBaseFlags("astore", "enkit")
	root := acommands.New(base)

	set, populator, runner := kcobra.Runner(root.Command, nil, base.IdentityErrorHandler("astore login"))

	rng := rand.New(srand.Source)
	root.AddCommand(bcommands.NewLogin(base, rng, populator).Command)

	base.Run(set, populator, runner)
}
