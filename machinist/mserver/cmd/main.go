package main

import (
	"github.com/ccontavalli/enkit/lib/client"
	"github.com/ccontavalli/enkit/lib/kflags/kcobra"
	"github.com/ccontavalli/enkit/machinist/mserver"
)

func main() {
	base := client.DefaultBaseFlags("astore", "enkit")

	root := mserver.NewCommand(base)
	set, populator, runner := kcobra.Runner(root, nil, base.IdentityErrorHandler("astore login"))

	base.Run(set, populator, runner)
}
