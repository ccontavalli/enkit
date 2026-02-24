package commands

import (
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/spf13/cobra"
)

func NewPutCommand(root *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "put <key>",
		Short: "Put a value by key",
		Args:  cobra.ExactArgs(1),
		Example: strings.TrimSpace(`
echo '{"enabled": true}' | enconfig put feature --src-app=myapp
enconfig put feature --src-app=myapp --input feature.json
`),
	}

	options := struct {
		Input string
	}{
		Input: "-",
	}

	cmd.Flags().StringVarP(&options.Input, "input", "i", options.Input, "Input file (default: stdin)")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		defer root.closeWorkspaces()

		data, err := readInputData(options.Input)
		if err != nil {
			return err
		}

		store, err := root.openStore(root.Source)
		if err != nil {
			return err
		}
		defer store.Close()

		var value interface{}
		if err := unmarshalByPath(options.Input, data, &value); err != nil {
			return err
		}

		return store.Marshal(config.Key(args[0]), value)
	}

	return cmd
}
