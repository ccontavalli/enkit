package main

import (
	"github.com/ccontavalli/enkit/lib/client"
	"github.com/ccontavalli/enkit/lib/kflags/kcobra"
	"github.com/ccontavalli/enkit/machinist"
)

func main() {
	base := client.DefaultBaseFlags("astore", "enkit")
	c := machinist.NewRootCommand(base)

	set, populator, runner := kcobra.Runner(c, nil, base.IdentityErrorHandler("enkit login"))

	base.Run(set, populator, runner)
}
