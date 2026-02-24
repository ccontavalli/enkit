package commands

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func NewRestoreCommand(root *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore a config store from a JSONL backup",
		Args:  cobra.NoArgs,
		Example: strings.TrimSpace(`
enconfig restore --dst-app=myapp --dst-namespace=prod --input backup.jsonl
cat backup.jsonl | enconfig restore --dst-app=myapp --recursive
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

		in, err := openInput(options.Input)
		if err != nil {
			return err
		}
		if in != os.Stdin {
			defer in.Close()
		}

		dec := json.NewDecoder(in)
		stores := map[string]storeCloser{}
		defer closeStores(stores)

		for {
			var rec backupRecord
			if err := dec.Decode(&rec); err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			nsKey := strings.Join(rec.Namespace, "/")
			store, ok := stores[nsKey]
			if !ok {
				s, err := root.openStoreNamespace(root.Dest, rec.Namespace)
				if err != nil {
					return err
				}
				store = storeCloser{Store: s}
				stores[nsKey] = store
			}
			if err := restoreRecord(store.Store, rec); err != nil {
				return err
			}
		}

		return nil
	}

	return cmd
}

func restoreRecord(store storeIface, rec backupRecord) error {
	var value interface{}
	if err := json.Unmarshal(rec.Value, &value); err != nil {
		return err
	}
	return store.Marshal(configKey(rec.Key), value)
}
