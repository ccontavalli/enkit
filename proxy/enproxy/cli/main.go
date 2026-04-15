package main

import (
	"fmt"
	"io"
	"math/rand"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/factory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/kflags/kcobra"
	"github.com/ccontavalli/enkit/lib/srand"
	"github.com/ccontavalli/enkit/proxy/enproxy"
	"github.com/spf13/cobra"
)

type ConfigFlags struct {
	Store     *factory.Flags
	Path      string
	RelayHost string
}

func DefaultConfigFlags() *ConfigFlags {
	return &ConfigFlags{
		Store: factory.DefaultAppConfigFlags(),
	}
}

func (cf *ConfigFlags) Register(set kflags.FlagSet) *ConfigFlags {
	cf.Store.Register(set, "")
	set.StringVar(&cf.Path, "config", cf.Path,
		"Path of the enproxy config entry to load from the configured config store. Directory-backed stores accept filesystem paths; other backends typically use app/ns/key.")
	set.StringVar(&cf.RelayHost, "host-port", cf.RelayHost,
		"Relay hostname and port to embed when upgrading legacy configs with tunnels.")
	return cf
}

func (cf *ConfigFlags) Open(rng *rand.Rand) (config.StoreWorkspace, config.Store, config.Binding, error) {
	path := strings.TrimSpace(cf.Path)
	storeFlags := cf.Store
	if path != "" && storeFlags.Directory.Path == "" {
		flagsClone := *storeFlags
		dirClone := *storeFlags.Directory
		dirClone.Path = "/"
		flagsClone.Directory = &dirClone
		storeFlags = &flagsClone
	}

	workspace, err := factory.NewStore(rng, factory.FromFlags(storeFlags))
	if err != nil {
		return nil, nil, nil, err
	}

	parsed := config.ParsedPath{}
	if path != "" {
		parsed, err = config.ResolvePathNative(workspace, path)
	} else {
		parsed, err = config.ResolvePathWithinStore(config.StoreRoot{AppName: "enproxy"}, "enproxy")
	}
	if err != nil {
		_ = workspace.Close()
		return nil, nil, nil, err
	}

	store, err := parsed.OpenStore(workspace)
	if err != nil {
		_ = workspace.Close()
		target := "enproxy/enproxy"
		if path != "" {
			target = path
		}
		return nil, nil, nil, fmt.Errorf("failed to open config namespace for %q: %w", target, err)
	}

	return workspace, store, parsed.Bind(store), nil
}

func loadConfig(binding config.Binding, relayHost string) (enproxy.Config, enproxy.Warnings, bool, error) {
	cfg, legacy, err := loadBinding(binding, relayHost)
	if err != nil {
		return enproxy.Config{}, nil, false, err
	}

	_, warnings, err := cfg.Parse()
	if err != nil {
		return enproxy.Config{}, nil, false, err
	}
	return cfg, warnings, legacy, nil
}

func loadLegacyConfig(binding config.Binding, relayHost string) (legacyConfig, error) {
	var legacy legacyConfig
	if err := binding.Unmarshal(&legacy); err != nil {
		return legacyConfig{}, err
	}
	if !legacy.looksLegacy() {
		return legacyConfig{}, kflags.NewUsageErrorf("selected config is not in the legacy enproxy format")
	}
	upgraded, err := legacy.upgrade(relayHost)
	if err != nil {
		return legacyConfig{}, err
	}
	if _, _, err := (&upgraded).Parse(); err != nil {
		return legacyConfig{}, err
	}
	return legacy, nil
}

func printWarnings(out io.Writer, warnings enproxy.Warnings) {
	for _, warning := range warnings {
		_, _ = fmt.Fprintln(out, warning)
	}
}

func marshalConfig(cfg enproxy.Config, format string) ([]byte, error) {
	marshaller := marshal.ByFormat(format)
	if marshaller == nil {
		return nil, kflags.NewUsageErrorf("unknown output format %q", format)
	}
	return marshaller.Marshal(cfg)
}

func NewConfigCheckCommand(rng *rand.Rand, flags *ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate the selected enproxy config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			workspace, store, binding, err := flags.Open(rng)
			if err != nil {
				return err
			}
			defer workspace.Close()
			defer store.Close()

			_, warnings, legacy, err := loadConfig(binding, flags.RelayHost)
			if err != nil {
				return err
			}
			if legacy {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning: selected config uses the legacy enproxy format; run `enproxyctl config update` to rewrite it")
			}
			printWarnings(cmd.ErrOrStderr(), warnings)
			return nil
		},
	}
	return cmd
}

func NewConfigPrintCommand(rng *rand.Rand, flags *ConfigFlags) *cobra.Command {
	format := "yaml"
	cmd := &cobra.Command{
		Use:   "print",
		Short: "Print the selected enproxy config after loading and normalization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			workspace, store, binding, err := flags.Open(rng)
			if err != nil {
				return err
			}
			defer workspace.Close()
			defer store.Close()

			cfg, warnings, legacy, err := loadConfig(binding, flags.RelayHost)
			if err != nil {
				return err
			}
			if legacy {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning: selected config uses the legacy enproxy format; printing the upgraded config in the current format")
			}
			printWarnings(cmd.ErrOrStderr(), warnings)

			data, err := marshalConfig(cfg, format)
			if err != nil {
				return err
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return err
			}
			if len(data) == 0 || data[len(data)-1] != '\n' {
				_, err = fmt.Fprintln(cmd.OutOrStdout())
			}
			return err
		},
	}
	cmd.Flags().StringVar(&format, "format", format, "Output format to use: yaml, json, or toml")
	return cmd
}

func NewConfigUpdateCommand(rng *rand.Rand, flags *ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Rewrite a legacy enproxy config into the current format",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			workspace, store, binding, err := flags.Open(rng)
			if err != nil {
				return err
			}
			defer workspace.Close()
			defer store.Close()

			legacy, err := loadLegacyConfig(binding, flags.RelayHost)
			if err != nil {
				return err
			}
			upgraded, err := legacy.upgrade(flags.RelayHost)
			if err != nil {
				return err
			}
			if err := binding.Marshal(upgraded); err != nil {
				return fmt.Errorf("failed to update config: %w", err)
			}
			return nil
		},
	}
	return cmd
}

func NewConfigCommand(rng *rand.Rand, flags *ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Query and rewrite enproxy configuration",
	}
	cmd.AddCommand(
		NewConfigCheckCommand(rng, flags),
		NewConfigPrintCommand(rng, flags),
		NewConfigUpdateCommand(rng, flags),
	)
	return cmd
}

func NewRoot(rng *rand.Rand) *cobra.Command {
	root := &cobra.Command{
		Use:           "enproxyctl",
		Short:         "Command-line tool for inspecting and managing enproxy",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	flags := DefaultConfigFlags().Register(&kcobra.FlagSet{FlagSet: root.PersistentFlags()})
	root.AddCommand(NewConfigCommand(rng, flags))
	return root
}

func main() {
	cobra.EnablePrefixMatching = true
	kcobra.Run(NewRoot(rand.New(srand.Source)))
}
