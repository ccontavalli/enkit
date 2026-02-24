package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/spf13/cobra"
)

type backupRecord struct {
	Namespace []string        `json:"namespace,omitempty"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
}

func NewBackupCommand(root *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup a config store to a JSONL file",
		Args:  cobra.NoArgs,
		Example: strings.TrimSpace(`
enconfig backup --src-app=myapp --src-namespace=prod --output backup.jsonl
enconfig backup --src-app=myapp --recursive > backup.jsonl
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

		namespaces, err := root.namespaces(root.Source, root.recursive)
		if err != nil {
			return err
		}

		out, err := openOutput(options.Output)
		if err != nil {
			return err
		}
		if out != os.Stdout {
			defer out.Close()
		}

		enc := json.NewEncoder(out)

		for _, ns := range namespaces {
			store, err := root.openStoreNamespace(root.Source, ns)
			if err != nil {
				return err
			}
			if err := backupNamespace(enc, store, ns); err != nil {
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

func backupNamespace(enc *json.Encoder, store config.Store, namespace []string) error {
	descs, err := store.List()
	if err != nil {
		return err
	}
	for _, desc := range descs {
		var value interface{}
		if _, err := store.Unmarshal(desc, &value); err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		rec := backupRecord{
			Namespace: append([]string(nil), namespace...),
			Key:       desc.Key(),
			Value:     raw,
		}
		if err := enc.Encode(&rec); err != nil {
			return fmt.Errorf("backup %s/%s: %w", strings.Join(namespace, "/"), desc.Key(), err)
		}
	}
	return nil
}
