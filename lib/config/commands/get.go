package commands

import (
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/spf13/cobra"
)

func NewGetCommand(root *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a value by key",
		Args:  cobra.ExactArgs(1),
		Example: strings.TrimSpace(`
enconfig get foo --src-app=myapp --src-namespace=prod
enconfig get foo --src-app=myapp --output -
enconfig get foo --src-app=myapp --output foo.json
`),
	}

	options := struct {
		Output string
	}{
		Output: "-",
	}

	cmd.Flags().StringVarP(&options.Output, "output", "o", options.Output, "Output file (default: stdout)")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		defer root.closeWorkspaces()

		store, err := root.openStore(root.Source)
		if err != nil {
			return err
		}
		defer store.Close()

		var value interface{}
		if _, err := store.Unmarshal(config.Key(args[0]), &value); err != nil {
			return err
		}

		data, err := marshalByPath(options.Output, value)
		if err != nil {
			return err
		}
		return writeOutputData(options.Output, data)
	}

	return cmd
}
