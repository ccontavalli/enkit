package cryptstore

import (
	"context"
	"fmt"
	"sort"

	"github.com/ccontavalli/enkit/lib/config"
)

// WrapLoader wraps a loader and panics on invalid modifiers.
func WrapLoader(loader config.Loader, mods ...Modifier) config.Loader {
	wrapped, err := NewLoader(loader, mods...)
	if err != nil {
		panic(err)
	}
	return wrapped
}

// WrapLoaderWorkspace wraps a loader workspace and panics on invalid modifiers.
func WrapLoaderWorkspace(workspace config.LoaderWorkspace, mods ...Modifier) config.LoaderWorkspace {
	wrapped, err := NewLoaderWorkspace(workspace, mods...)
	if err != nil {
		panic(err)
	}
	return wrapped
}

// NewLoader wraps a loader with key/value encryption.
//
// For structured values, compose the returned loader with config.OpenSimple or config.OpenMulti.
func NewLoader(loader config.Loader, mods ...Modifier) (config.Loader, error) {
	if loader == nil {
		return nil, fmt.Errorf("loader is required")
	}
	opts := defaultOptions()
	if err := Modifiers(mods).Apply(&opts); err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	return &loaderWrap{loader: loader, options: opts}, nil
}

// NewLoaderWorkspace wraps a loader workspace with key/value encryption.
//
// For structured values, compose the opened loaders with config.NewSimple or config.NewMulti.
func NewLoaderWorkspace(workspace config.LoaderWorkspace, mods ...Modifier) (config.LoaderWorkspace, error) {
	if workspace == nil {
		return nil, fmt.Errorf("workspace is required")
	}
	opts := defaultOptions()
	if err := Modifiers(mods).Apply(&opts); err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	return &loaderWorkspaceWrap{workspace: workspace, options: opts}, nil
}

type loaderWrap struct {
	loader config.Loader
	options
}

func (w *loaderWrap) List(mods ...config.ListModifier) ([]string, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	if optimizing, ok := w.keyCodec.(ListOptimizingKeyCodec); ok {
		keys, optimized, err := optimizing.List(w.loader.List, *opts)
		if err != nil {
			return nil, err
		}
		return opts.FinalizeKeys(w, keys, optimized)
	}

	keys, err := w.loader.List()
	if err != nil {
		return nil, err
	}
	decoded := make([]string, len(keys))
	for i, key := range keys {
		decodedKey, err := w.keyCodec.Decode(key)
		if err != nil {
			return nil, err
		}
		decoded[i] = decodedKey
	}
	sort.Strings(decoded)
	return opts.FinalizeKeys(w, decoded, 0)
}

func (w *loaderWrap) Read(name string) ([]byte, error) {
	encoded, err := w.keyCodec.Encode(name)
	if err != nil {
		return nil, err
	}
	ciphertext, err := w.loader.Read(encoded)
	if err != nil {
		return nil, err
	}
	_, plain, err := w.valueEncoder.Decode(context.Background(), ciphertext)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

func (w *loaderWrap) Write(name string, data []byte) error {
	encoded, err := w.keyCodec.Encode(name)
	if err != nil {
		return err
	}
	ciphertext, err := w.valueEncoder.Encode(data)
	if err != nil {
		return err
	}
	return w.loader.Write(encoded, ciphertext)
}

func (w *loaderWrap) Delete(name string) error {
	encoded, err := w.keyCodec.Encode(name)
	if err != nil {
		return err
	}
	return w.loader.Delete(encoded)
}

func (w *loaderWrap) Close() error {
	return w.loader.Close()
}

type loaderWorkspaceWrap struct {
	workspace config.LoaderWorkspace
	options
}

func (w *loaderWorkspaceWrap) Open(name string, namespace ...string) (config.Loader, error) {
	loader, err := w.workspace.Open(name, namespace...)
	if err != nil {
		return nil, err
	}
	return &loaderWrap{loader: loader, options: w.options}, nil
}

func (w *loaderWorkspaceWrap) Explore(name string, namespace ...string) (config.Explorer, error) {
	return w.workspace.Explore(name, namespace...)
}

func (w *loaderWorkspaceWrap) Close() error {
	return w.workspace.Close()
}
