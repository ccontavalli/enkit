package commands

import (
	"fmt"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/spf13/cobra"
)

func NewListCommand(root *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List keys in a config store",
		Args:  cobra.NoArgs,
		Example: strings.TrimSpace(`
enconfig list --src-app=myapp
enconfig list --src-app=myapp --src-namespace=prod
enconfig list --src-app=myapp --recursive
`),
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		defer root.closeWorkspaces()

		namespaces, err := root.namespaces(root.Source, root.recursive)
		if err != nil {
			return err
		}

		for _, ns := range namespaces {
			store, err := root.openStoreNamespace(root.Source, ns)
			if err != nil {
				return err
			}
			if err := listNamespace(store, ns); err != nil {
				_ = store.Close()
				return err
			}
			if err := store.Close(); err != nil {
				return err
			}
		}
		return nil
	}

	return cmd
}

func listNamespace(store config.Store, namespace []string) error {
	descs, err := store.List()
	if err != nil {
		return err
	}
	if len(namespace) > 0 {
		fmt.Printf("namespace: %s\n", strings.Join(namespace, "/"))
	} else {
		fmt.Printf("namespace: /\n")
	}
	for _, desc := range descs {
		fmt.Printf("  %s\n", desc.Key())
	}
	return nil
}
