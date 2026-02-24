package commands

import (
	"path"
	"strings"

	"github.com/spf13/cobra"
)

func NewFindCommand(root *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "find <pattern>",
		Short: "Find keys matching a pattern",
		Args:  cobra.ExactArgs(1),
		Example: strings.TrimSpace(`
enconfig find "*.json" --src-app=myapp --recursive
enconfig find "foo-*" --src-app=myapp --src-namespace=prod
`),
	}

	options := struct {
		IgnoreCase bool
	}{
		IgnoreCase: false,
	}

	cmd.Flags().BoolVarP(&options.IgnoreCase, "ignore-case", "i", options.IgnoreCase, "Case-insensitive match")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		defer root.closeWorkspaces()

		pattern := args[0]
		namespaces, err := root.namespaces(root.Source, root.recursive)
		if err != nil {
			return err
		}

		for _, ns := range namespaces {
			store, err := root.openStoreNamespace(root.Source, ns)
			if err != nil {
				return err
			}
			if err := findNamespace(store, ns, pattern, options.IgnoreCase); err != nil {
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

func findNamespace(store storeIfaceList, namespace []string, pattern string, ignoreCase bool) error {
	descs, err := store.List()
	if err != nil {
		return err
	}
	for _, desc := range descs {
		key := desc.Key()
		matchKey := key
		matchPattern := pattern
		if ignoreCase {
			matchKey = strings.ToLower(matchKey)
			matchPattern = strings.ToLower(matchPattern)
		}
		ok, err := path.Match(matchPattern, matchKey)
		if err != nil {
			return err
		}
		if ok {
			printKey(namespace, key)
		}
	}
	return nil
}
