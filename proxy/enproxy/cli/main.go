package main

import (
	"fmt"
	"io"
	"math/rand"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/kflags/kcobra"
	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/lib/srand"
	"github.com/ccontavalli/enkit/proxy/enproxy"
	"github.com/spf13/cobra"
)

const legacyConfigMessage = "selected config uses the legacy enproxy format; run `enproxyctl config update` first"

func rejectLegacyConfig() error {
	return kflags.NewUsageErrorf(legacyConfigMessage)
}

func currentConfigOrRejectLegacy(binding config.Binding, normalizer *enproxy.ConfigNormalizer) (enproxy.Config, enproxy.Warnings, error) {
	current, warnings, err := normalizer.ParseConfigBinding(binding)
	if err == nil {
		return current, warnings, nil
	}

	var legacy legacyConfig
	if legacyErr := binding.Unmarshal(&legacy); legacyErr == nil && legacy.looksLegacy() {
		return enproxy.Config{}, nil, rejectLegacyConfig()
	}
	return enproxy.Config{}, nil, err
}

func loadLegacyConfig(binding config.Binding, relayHost string) (legacyConfig, error) {
	var legacy legacyConfig
	if err := binding.Unmarshal(&legacy); err == nil && legacy.looksLegacy() {
		upgraded, err := legacy.upgrade(relayHost)
		if err != nil {
			return legacyConfig{}, err
		}
		if _, _, err := (&upgraded).Parse(); err != nil {
			return legacyConfig{}, err
		}
		return legacy, nil
	}

	if _, _, err := enproxy.ParseConfigBinding(binding, relayHost); err == nil {
		return legacyConfig{}, kflags.NewUsageErrorf("selected config is already in the current enproxy format")
	}

	if err := binding.Unmarshal(&legacy); err != nil {
		return legacyConfig{}, err
	}
	return legacyConfig{}, kflags.NewUsageErrorf("selected config is not in the legacy enproxy format")
}

func printWarnings(out io.Writer, warnings enproxy.Warnings) {
	for _, warning := range warnings {
		_, _ = fmt.Fprintln(out, warning)
	}
}

func loadableCurrentConfig(rng *rand.Rand, flags *enproxy.Flags, current enproxy.Config) error {
	mods, err := enproxy.RuntimeModifiersFromFlags(flags)
	if err != nil {
		return err
	}
	mods = append(mods, enproxy.WithConfig(current), enproxy.WithLogging(logger.Nil))
	ep, err := enproxy.New(rng, mods...)
	if err != nil {
		return err
	}
	return ep.Close()
}

func NewConfigCheckCommand(rng *rand.Rand, flags *enproxy.Flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate the selected enproxy config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizer, err := enproxy.NewConfigNormalizer(
				strings.TrimSpace(flags.Nassh.RelayHost),
				flags.DisabledAuthentication,
				flags.UnsafeIgnoreAuthentication,
			)
			if err != nil {
				return err
			}

			workspace, store, binding, _, err := enproxy.OpenConfigBinding(rng, flags)
			if err != nil {
				return err
			}
			defer workspace.Close()
			defer store.Close()

			current, warnings, err := currentConfigOrRejectLegacy(binding, normalizer)
			if err != nil {
				if err.Error() == legacyConfigMessage {
					return err
				}
				return fmt.Errorf("selected config is not loadable: %w", err)
			}
			printWarnings(cmd.ErrOrStderr(), warnings)
			if err := loadableCurrentConfig(rng, flags, current); err != nil {
				return fmt.Errorf("selected config is not loadable: %w", err)
			}
			return nil
		},
	}
	return cmd
}

func NewConfigPrintCommand(rng *rand.Rand, flags *enproxy.Flags) *cobra.Command {
	format := "yaml"
	cmd := &cobra.Command{
		Use:   "print",
		Short: "Print the selected effective enproxy config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizer, err := enproxy.NewConfigNormalizer(
				strings.TrimSpace(flags.Nassh.RelayHost),
				flags.DisabledAuthentication,
				flags.UnsafeIgnoreAuthentication,
			)
			if err != nil {
				return err
			}

			workspace, store, binding, _, err := enproxy.OpenConfigBinding(rng, flags)
			if err != nil {
				return err
			}
			defer workspace.Close()
			defer store.Close()

			current, warnings, err := currentConfigOrRejectLegacy(binding, normalizer)
			if err != nil {
				return err
			}
			printWarnings(cmd.ErrOrStderr(), warnings)

			marshaller := marshal.ByFormat(format)
			if marshaller == nil {
				return kflags.NewUsageErrorf("unknown output format %q", format)
			}
			data, err := marshaller.Marshal(current)
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

func NewConfigUpdateCommand(rng *rand.Rand, flags *enproxy.Flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Rewrite a legacy enproxy config into the current format",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			workspace, store, binding, _, err := enproxy.OpenConfigBinding(rng, flags)
			if err != nil {
				return err
			}
			defer workspace.Close()
			defer store.Close()

			relayHost := strings.TrimSpace(flags.Nassh.RelayHost)
			legacy, err := loadLegacyConfig(binding, relayHost)
			if err != nil {
				return err
			}
			upgraded, err := legacy.upgrade(relayHost)
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

func NewConfigCommand(rng *rand.Rand, flags *enproxy.Flags) *cobra.Command {
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

	flags := enproxy.DefaultFlags().Register(&kcobra.FlagSet{FlagSet: root.PersistentFlags()}, "")
	root.AddCommand(NewConfigCommand(rng, flags))
	return root
}

func main() {
	cobra.EnablePrefixMatching = true
	kcobra.Run(NewRoot(rand.New(srand.Source)))
}
