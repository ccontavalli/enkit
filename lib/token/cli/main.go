package main

import (
	"fmt"
	"github.com/ccontavalli/enkit/lib/kflags/kcobra"
	"github.com/ccontavalli/enkit/lib/srand"
	"github.com/ccontavalli/enkit/lib/token"
	"github.com/spf13/cobra"
	"io/ioutil"
	"math/rand"
)

func RegisterSymmetric(root *cobra.Command) {
}

func CreateSigning(rng *rand.Rand) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "symmetric",
		Short: "Commands to deal with symmetric tokens",
	}
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generates an signing key",
	}

	options := struct {
		KeyFile string
		Bits    int
	}{}

	generate.Flags().StringVarP(&options.KeyFile, "key-file", "k", "", "Path where to store the key")
	generate.Flags().IntVarP(&options.Bits, "bits", "b", 256, "How long of a key to generate")

	generate.RunE = func(cmd *cobra.Command, args []string) error {
		key, err := token.GenerateSymmetricKey(rng, options.Bits)
		if err != nil {
			return err
		}

		if options.KeyFile != "" {
			if err := ioutil.WriteFile(options.KeyFile, key, 0400); err != nil {
				return fmt.Errorf("couldn't save verifying key: %w", err)
			}
		} else {
			fmt.Printf("key: %064x\n", key)
		}

		return nil
	}

	cmd.AddCommand(generate)
	return cmd
}

func CreateSymmetric(rng *rand.Rand) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "signing",
		Short: "Commands to deal with signing (asymmetric) tokens",
	}
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generates a signing and verifying key pair",
		Args:  cobra.NoArgs,
	}

	options := struct {
		SigningKeyFile   string
		VerifyingKeyFile string
	}{}

	generate.Flags().StringVarP(&options.SigningKeyFile, "signing-key-file", "s", "", "Path where to store the signing key")
	generate.Flags().StringVarP(&options.VerifyingKeyFile, "verifying-key-file", "f", "", "Path where to store the verifying key")

	generate.RunE = func(cmd *cobra.Command, args []string) error {
		verify, sign, err := token.GenerateSigningKey(rng)
		if err != nil {
			return err
		}

		if options.VerifyingKeyFile != "" {
			if err := ioutil.WriteFile(options.VerifyingKeyFile, (*verify.ToBytes())[:], 0400); err != nil {
				return fmt.Errorf("couldn't save verifying key: %w", err)
			}
		} else {
			fmt.Printf("verifying: %064x\n", *verify)
		}

		if options.SigningKeyFile != "" {
			if err := ioutil.WriteFile(options.SigningKeyFile, (*sign.ToBytes())[:], 0400); err != nil {
				return fmt.Errorf("couldn't save signing key: %w", err)
			}
		} else {
			fmt.Printf("signing: %0128x\n", *sign)
		}
		return nil
	}

	cmd.AddCommand(generate)
	return cmd
}

func CreateAsymmetric(rng *rand.Rand) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "asymmetric",
		Short: "Commands to deal with encrypting asymmetric tokens",
	}
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generates a public and private key pair",
		Args:  cobra.NoArgs,
	}

	options := struct {
		PublicKeyFile  string
		PrivateKeyFile string
	}{}

	generate.Flags().StringVarP(&options.PublicKeyFile, "public-key-file", "s", "", "Path where to store the public key")
	generate.Flags().StringVarP(&options.PrivateKeyFile, "private-key-file", "f", "", "Path where to store the private key")

	generate.RunE = func(cmd *cobra.Command, args []string) error {
		pub, priv, err := token.GenerateAsymmetricKeys(rng)
		if err != nil {
			return err
		}

		if options.PublicKeyFile != "" {
			if err := ioutil.WriteFile(options.PublicKeyFile, (*pub.ToByte())[:], 0400); err != nil {
				return fmt.Errorf("couldn't save public key: %w", err)
			}
		} else {
			fmt.Printf("public: %064x\n", *pub)
		}

		if options.PrivateKeyFile != "" {
			if err := ioutil.WriteFile(options.PrivateKeyFile, (*priv.ToByte())[:], 0400); err != nil {
				return fmt.Errorf("couldn't save private key: %w", err)
			}
		} else {
			fmt.Printf("private: %064x\n", *priv)
		}
		return nil
	}

	cmd.AddCommand(generate)
	return cmd
}

func main() {
	rng := rand.New(srand.Source)

	root := &cobra.Command{
		Use:   "entoken",
		Short: "Tool to help dealing with cryptographic enkit tokens",
	}

	root.AddCommand(CreateSymmetric(rng))
	root.AddCommand(CreateSigning(rng))
	root.AddCommand(CreateAsymmetric(rng))

	cobra.EnablePrefixMatching = true
	kcobra.Run(root)
}
