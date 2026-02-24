package config

import (
	"fmt"
	"sort"
)

// NamespaceDescriptor is a descriptor that carries a full namespace path.
type NamespaceDescriptor interface {
	Descriptor
	NamespacePath() []string
}

type namespaceDescriptor struct {
	name string
	path []string
}

// NewNamespaceDescriptor returns a Descriptor for a namespace path.
func NewNamespaceDescriptor(path []string) NamespaceDescriptor {
	name := ""
	if len(path) > 0 {
		name = path[len(path)-1]
	}
	return namespaceDescriptor{name: name, path: append([]string(nil), path...)}
}

func (d namespaceDescriptor) Key() string {
	return d.name
}

func (d namespaceDescriptor) NamespacePath() []string {
	return append([]string(nil), d.path...)
}

// NamespaceList returns the child namespace names under the given path.
func NamespaceList(explorer StoreWorkspace, name string, namespace ...string) ([]string, error) {
	store, err := explorer.Explore(name, namespace...)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	descs, err := store.List()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(descs))
	for _, desc := range descs {
		out = append(out, desc.Key())
	}
	sort.Strings(out)
	return out, nil
}

// NamespaceExists reports whether a namespace exists under its parent.
func NamespaceExists(explorer StoreWorkspace, name string, namespace ...string) (bool, error) {
	if len(namespace) == 0 {
		return false, fmt.Errorf("namespace path is required")
	}
	parent := namespace[:len(namespace)-1]
	child := namespace[len(namespace)-1]
	store, err := explorer.Explore(name, parent...)
	if err != nil {
		return false, err
	}
	defer store.Close()

	descs, err := store.List()
	if err != nil {
		return false, err
	}
	for _, desc := range descs {
		if desc.Key() == child {
			return true, nil
		}
	}
	return false, nil
}

// NamespaceWalk walks namespaces depth-first, calling fn with full paths.
func NamespaceWalk(explorer StoreWorkspace, name string, namespace []string, fn func([]string) error) error {
	store, err := explorer.Explore(name, namespace...)
	if err != nil {
		return err
	}
	defer store.Close()

	descs, err := store.List()
	if err != nil {
		return err
	}
	for _, desc := range descs {
		path := NamespacePathFromDescriptor(namespace, desc)
		if err := fn(path); err != nil {
			return err
		}
		if err := NamespaceWalk(explorer, name, path, fn); err != nil {
			return err
		}
	}
	return nil
}

// NamespaceDelete removes a namespace by deleting it from its parent.
func NamespaceDelete(explorer StoreWorkspace, name string, namespace ...string) error {
	if len(namespace) == 0 {
		return fmt.Errorf("namespace path is required")
	}
	parent := namespace[:len(namespace)-1]
	store, err := explorer.Explore(name, parent...)
	if err != nil {
		return err
	}
	defer store.Close()
	return store.Delete(NewNamespaceDescriptor(append([]string(nil), namespace...)))
}

// NamespacePathFromDescriptor returns the full namespace path for a descriptor.
func NamespacePathFromDescriptor(base []string, desc Descriptor) []string {
	if ns, ok := desc.(NamespaceDescriptor); ok {
		return ns.NamespacePath()
	}
	return append(append([]string(nil), base...), desc.Key())
}

// NamespaceDescriptors builds descriptors for child namespaces.
func NamespaceDescriptors(base []string, children []string) []Descriptor {
	descs := make([]Descriptor, len(children))
	for i, child := range children {
		path := append(append([]string(nil), base...), child)
		descs[i] = NewNamespaceDescriptor(path)
	}
	return descs
}

// SortedNamespaceDescriptors returns sorted namespace descriptors from child names.
func SortedNamespaceDescriptors(base []string, children []string) []Descriptor {
	return NamespaceDescriptors(base, SortedKeys(children))
}
