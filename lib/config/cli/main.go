package main

import (
	"github.com/ccontavalli/enkit/lib/config/commands"
	"github.com/ccontavalli/enkit/lib/kflags/kcobra"
	"github.com/spf13/cobra"
)

func main() {
	root := commands.NewRoot()
	cobra.EnablePrefixMatching = true
	kcobra.Run(root.Command)
}
