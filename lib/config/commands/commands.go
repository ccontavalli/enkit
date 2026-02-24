package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/factory"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/kflags/kcobra"
	"github.com/spf13/cobra"
)

// StoreFlags wraps config factory flags with app/namespace parameters.
type StoreFlags struct {
	Flags     *factory.Flags
	App       string
	Namespace []string
	Prefix    string
}

// DefaultStoreFlags returns a new StoreFlags with defaults.
func DefaultStoreFlags() *StoreFlags {
	return &StoreFlags{
		Flags: factory.DefaultFlags(),
	}
}

// Register registers store flags with a prefix.
func (sf *StoreFlags) Register(set kflags.FlagSet, prefix string) *StoreFlags {
	sf.Prefix = prefix
	sf.Flags.Register(set, prefix)
	set.StringVar(&sf.App, prefix+"app", sf.App, "App name for the config store")
	set.StringArrayVar(&sf.Namespace, prefix+"namespace", sf.Namespace, "Namespace path (repeatable)")
	return sf
}

// Root is the root command for config store utilities.
type Root struct {
	*cobra.Command
	Source     *StoreFlags
	Dest       *StoreFlags
	recursive  bool
	workspaces map[*factory.Flags]config.StoreWorkspace
}

// NewRoot returns a configured root command.
func NewRoot() *Root {
	root := &Root{
		Command: &cobra.Command{
			Use:           "enconfig",
			Short:         "Explore, backup, restore, and convert config stores",
			SilenceUsage:  true,
			SilenceErrors: true,
		},
		Source:     DefaultStoreFlags(),
		Dest:       DefaultStoreFlags(),
		workspaces: map[*factory.Flags]config.StoreWorkspace{},
	}

	root.Source.Register(&kcobra.FlagSet{FlagSet: root.PersistentFlags()}, "src-")
	root.Dest.Register(&kcobra.FlagSet{FlagSet: root.PersistentFlags()}, "dst-")
	root.PersistentFlags().BoolVar(&root.recursive, "recursive", false, "Recurse into child namespaces")

	root.AddCommand(NewListCommand(root))
	root.AddCommand(NewBackupCommand(root))
	root.AddCommand(NewRestoreCommand(root))
	root.AddCommand(NewConvertCommand(root))
	root.AddCommand(NewFindCommand(root))
	root.AddCommand(NewGrepCommand(root))
	root.AddCommand(NewGetCommand(root))
	root.AddCommand(NewPutCommand(root))

	return root
}

func (r *Root) openStore(sf *StoreFlags) (config.Store, error) {
	if sf.App == "" {
		return nil, kflags.NewUsageErrorf("must specify --%sapp", sf.Prefix)
	}
	workspace, err := r.workspace(sf)
	if err != nil {
		return nil, err
	}
	return workspace.Open(sf.App, sf.Namespace...)
}

func (r *Root) openStoreNamespace(sf *StoreFlags, namespace []string) (config.Store, error) {
	copy := *sf
	copy.Namespace = namespace
	return r.openStore(&copy)
}

func (r *Root) workspace(sf *StoreFlags) (config.StoreWorkspace, error) {
	if workspace, ok := r.workspaces[sf.Flags]; ok {
		return workspace, nil
	}
	if sf.App == "" {
		return nil, kflags.NewUsageErrorf("must specify --%sapp", sf.Prefix)
	}
	workspace, err := factory.NewStore(factory.FromFlags(sf.Flags))
	if err != nil {
		return nil, err
	}
	r.workspaces[sf.Flags] = workspace
	return workspace, nil
}

func (r *Root) namespaces(sf *StoreFlags, recursive bool) ([][]string, error) {
	if !recursive {
		return [][]string{append([]string(nil), sf.Namespace...)}, nil
	}

	explorer, err := r.workspace(sf)
	if err != nil {
		return nil, err
	}
	var paths [][]string
	err = config.NamespaceWalk(explorer, sf.App, sf.Namespace, func(path []string) error {
		paths = append(paths, append([]string(nil), path...))
		return nil
	})
	if err != nil {
		return nil, err
	}
	paths = append(paths, append([]string(nil), sf.Namespace...))
	return paths, nil
}

func (r *Root) closeWorkspaces() {
	for _, workspace := range r.workspaces {
		_ = workspace.Close()
	}
}

func relativeNamespace(base, full []string) ([]string, error) {
	if len(base) > len(full) {
		return nil, fmt.Errorf("namespace %s not within %s", strings.Join(full, "/"), strings.Join(base, "/"))
	}
	for i := range base {
		if base[i] != full[i] {
			return nil, fmt.Errorf("namespace %s not within %s", strings.Join(full, "/"), strings.Join(base, "/"))
		}
	}
	return append([]string(nil), full[len(base):]...), nil
}

func openOutput(path string) (*os.File, error) {
	if path == "" || path == "-" {
		return os.Stdout, nil
	}
	return os.Create(path)
}

func openInput(path string) (*os.File, error) {
	if path == "" || path == "-" {
		return os.Stdin, nil
	}
	return os.Open(path)
}
