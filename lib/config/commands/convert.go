package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/progress"
	"github.com/spf13/cobra"
)

func NewConvertCommand(root *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Copy data from one store to another",
		Args:  cobra.NoArgs,
		Example: strings.TrimSpace(`
enconfig convert --src-app=myapp --dst-app=myapp --dst-namespace=backup
enconfig convert --src-app=myapp --src-namespace=prod --dst-app=myapp --dst-namespace=prod-copy --recursive
`),
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		defer root.closeWorkspaces()

		namespaces, err := root.namespaces(root.Source, root.recursive)
		if err != nil {
			return err
		}

		for _, ns := range namespaces {
			rel, err := relativeNamespace(root.Source.Namespace, ns)
			if err != nil {
				return err
			}
			dstNamespace := append(append([]string(nil), root.Dest.Namespace...), rel...)
			if err := convertNamespace(root, ns, dstNamespace); err != nil {
				return err
			}
		}
		return nil
	}

	return cmd
}

func convertNamespace(root *Root, srcNamespace []string, dstNamespace []string) error {
	srcStore, err := root.openStoreNamespace(root.Source, srcNamespace)
	if err != nil {
		return err
	}
	defer srcStore.Close()

	dstStore, err := root.openStoreNamespace(root.Dest, dstNamespace)
	if err != nil {
		return err
	}
	defer dstStore.Close()

	descs, err := srcStore.List()
	if err != nil {
		return err
	}
	progressWriter := newProgressWriter(int64(len(descs)))
	defer progressWriter.Close()
	for _, desc := range descs {
		var value interface{}
		if _, err := srcStore.Unmarshal(desc, &value); err != nil {
			return err
		}
		if err := dstStore.Marshal(config.Key(desc.Key()), value); err != nil {
			return fmt.Errorf("copy %s/%s: %w", strings.Join(srcNamespace, "/"), desc.Key(), err)
		}
		if _, err := progressWriter.Write([]byte{0}); err != nil {
			return err
		}
	}
	return nil
}

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error { return nil }

func newProgressWriter(total int64) io.WriteCloser {
	bar := progress.NewBar()
	return bar.Writer(nopWriteCloser{Writer: io.Discard}, total)
}
