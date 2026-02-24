package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/marshal"
)

type storeIface interface {
	Marshal(desc config.Descriptor, value interface{}) error
}

type storeIfaceList interface {
	List(mods ...config.ListModifier) ([]config.Descriptor, error)
}

type storeCloser struct {
	config.Store
}

func closeStores(stores map[string]storeCloser) {
	for _, store := range stores {
		_ = store.Close()
	}
}

func configKey(key string) config.Descriptor {
	return config.Key(key)
}

func readInputData(path string) ([]byte, error) {
	in, err := openInput(path)
	if err != nil {
		return nil, err
	}
	if in != os.Stdin {
		defer in.Close()
	}
	return io.ReadAll(in)
}

func writeOutputData(path string, data []byte) error {
	out, err := openOutput(path)
	if err != nil {
		return err
	}
	if out != os.Stdout {
		defer out.Close()
	}
	_, err = out.Write(data)
	return err
}

func marshalByPath(path string, value interface{}) ([]byte, error) {
	return marshal.FileMarshallers(marshal.Known).MarshalDefault(path, marshal.Json, value)
}

func unmarshalByPath(path string, data []byte, value interface{}) error {
	return marshal.FileMarshallers(marshal.Known).UnmarshalDefault(path, data, marshal.Json, value)
}

func printKey(namespace []string, key string) {
	if len(namespace) == 0 {
		fmt.Printf("/%s\n", key)
		return
	}
	fmt.Printf("%s/%s\n", strings.Join(namespace, "/"), key)
}

func printKeyLine(namespace []string, key string, line string) {
	if len(namespace) == 0 {
		fmt.Printf("/%s: %s\n", key, line)
		return
	}
	fmt.Printf("%s/%s: %s\n", strings.Join(namespace, "/"), key, line)
}
