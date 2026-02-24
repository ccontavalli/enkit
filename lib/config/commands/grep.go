package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/spf13/cobra"
)

var errGrepNoMatch = errors.New("no matches")

func NewGrepCommand(root *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grep <pattern>",
		Short: "Search values like grep",
		Args:  cobra.ExactArgs(1),
		Example: strings.TrimSpace(`
enconfig grep '"feature": true' --src-app=myapp --recursive
enconfig grep -i "timeout" --src-app=myapp --src-namespace=prod
enconfig grep -l "owner" --src-app=myapp
`),
	}

	options := struct {
		IgnoreCase bool
		ListOnly   bool
		Quiet      bool
		Recursive  bool
	}{
		IgnoreCase: false,
		ListOnly:   false,
		Quiet:      false,
		Recursive:  false,
	}

	cmd.Flags().BoolVarP(&options.IgnoreCase, "ignore-case", "i", options.IgnoreCase, "Case-insensitive match")
	cmd.Flags().BoolVarP(&options.ListOnly, "files-with-matches", "l", options.ListOnly, "Print only keys with matches")
	cmd.Flags().BoolVarP(&options.Quiet, "quiet", "q", options.Quiet, "Exit immediately with status 0 on first match")
	cmd.Flags().BoolVarP(&options.Recursive, "recursive", "r", options.Recursive, "Recurse into child namespaces")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		defer root.closeWorkspaces()

		pattern := args[0]
		if options.IgnoreCase {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return err
		}

		recursive := root.recursive || options.Recursive
		namespaces, err := root.namespaces(root.Source, recursive)
		if err != nil {
			return err
		}

		matched := false
		for _, ns := range namespaces {
			store, err := root.openStoreNamespace(root.Source, ns)
			if err != nil {
				return err
			}
			err = grepNamespace(store, ns, re, options.ListOnly, options.Quiet, &matched)
			if err != nil {
				_ = store.Close()
				return err
			}
			if err := store.Close(); err != nil {
				return err
			}
			if options.Quiet && matched {
				return nil
			}
		}

		if options.Quiet && !matched {
			return errGrepNoMatch
		}
		return nil
	}

	return cmd
}

func grepNamespace(store config.Store, namespace []string, re *regexp.Regexp, listOnly bool, quiet bool, matched *bool) error {
	descs, err := store.List()
	if err != nil {
		return err
	}
	for _, desc := range descs {
		data, err := readValueBytes(store, desc)
		if err != nil {
			return err
		}
		if !re.Match(data) {
			continue
		}
		*matched = true
		if quiet {
			return nil
		}
		if listOnly {
			printKey(namespace, desc.Key())
			continue
		}
		if err := printMatches(namespace, desc.Key(), data, re); err != nil {
			return err
		}
	}
	return nil
}

func readValueBytes(store config.Store, desc config.Descriptor) ([]byte, error) {
	var raw json.RawMessage
	if _, err := store.Unmarshal(desc, &raw); err == nil && raw != nil {
		return raw, nil
	}
	var value interface{}
	if _, err := store.Unmarshal(desc, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func printMatches(namespace []string, key string, data []byte, re *regexp.Regexp) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if re.MatchString(line) {
			printKeyLine(namespace, key, line)
		}
	}
	return scanner.Err()
}
