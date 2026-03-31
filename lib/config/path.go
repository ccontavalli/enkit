package config

import (
	"fmt"
	"path"
	"strings"

	"github.com/ccontavalli/enkit/lib/config/marshal"
)

// ParsedPath identifies a namespace store and a descriptor within it.
//
// It is the resolved form produced by ParsePath helpers. StoreRoot identifies
// which store to open, while Descriptor identifies the entry within that store.
// The descriptor may be a plain Key or a richer descriptor carrying backend or
// format-specific metadata.
type ParsedPath struct {
	StoreRoot
	Descriptor Descriptor
}

// StoreRoot identifies the namespace root of a store chosen by the application.
//
// It is used with ResolvePathWithinStore to interpret --config-like flags as
// remaining inside the selected store boundary rather than using backend-native
// absolute path semantics.
type StoreRoot struct {
	AppName    string
	Namespaces []string
}

// OpenStore opens the namespace store identified by the path.
func (p ParsedPath) OpenStore(ws StoreWorkspace) (Store, error) {
	return ws.Open(p.AppName, p.Namespaces...)
}

// Bind creates a binding within the provided store using the resolved descriptor.
func (p ParsedPath) Bind(store Store) Binding {
	return Bind(store, p.Descriptor)
}

// RequestedFormatDescriptor is a descriptor that carries an explicit requested
// serialization format, usually derived from a file extension in the parsed
// path.
//
// Simple stores use it to reject mismatched extensions, while multi-format
// stores use it to select the requested marshaller instead of defaulting by
// preference order.
type RequestedFormatDescriptor interface {
	Descriptor
	Format() string
}

type formatHintDescriptor struct {
	key    string
	format string
}

func (d formatHintDescriptor) Key() string {
	return d.key
}

func (d formatHintDescriptor) Format() string {
	return d.format
}

// RequestedFormatKey returns a descriptor for key with an optional requested
// format.
//
// If format is empty, the result is a plain Key. Otherwise the descriptor also
// records the requested format so store implementations can enforce or honor it
// later.
func RequestedFormatKey(key string, format string) Descriptor {
	if format == "" {
		return Key(key)
	}
	return formatHintDescriptor{key: key, format: format}
}

// DefaultParsePath parses a logical path using the generic app/ns/key format.
func DefaultParsePath(path string) (ParsedPath, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ParsedPath{}, fmt.Errorf("config path cannot be empty")
	}

	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ParsedPath{}, fmt.Errorf("config path %q must contain at least app/key", path)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return ParsedPath{}, fmt.Errorf("invalid config path %q", path)
		}
	}
	key := DecodeKey(parts[len(parts)-1])

	return ParsedPath{
		StoreRoot: StoreRoot{
			AppName:    parts[0],
			Namespaces: append([]string(nil), parts[1:len(parts)-1]...),
		},
		Descriptor: Key(key),
	}, nil
}

// ResolvePathNative resolves a path using the backend-native parser of the
// provided workspace.
//
// Example:
//
//	parsed, err := config.ResolvePathNative(ws, "enproxy/runtime/config")
func ResolvePathNative(ws StoreWorkspace, path string) (ParsedPath, error) {
	if ws == nil {
		return ParsedPath{}, fmt.Errorf("config workspace cannot be nil")
	}
	return ws.ParsePath(path)
}

// ResolvePathWithinStore resolves a path within an application-selected store
// root.
//
// The path syntax is logical and slash-separated rather than filesystem-based.
// Relative paths can descend into child namespaces. Absolute paths, home-based
// paths, and ".." escapes are rejected so the result stays inside the selected
// store boundary. The final path segment is decoded with DecodeKey so escaped
// keys such as "config%2Fprod.yaml" resolve to the key "config/prod".
//
// Example:
//
//	root := config.StoreRoot{AppName: "enproxy", Namespaces: []string{"runtime"}}
//	parsed, err := config.ResolvePathWithinStore(root, "admin/config.yaml")
func ResolvePathWithinStore(root StoreRoot, name string) (ParsedPath, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ParsedPath{}, fmt.Errorf("config path cannot be empty")
	}
	if root.AppName == "" {
		return ParsedPath{}, fmt.Errorf("store root app name cannot be empty")
	}
	if strings.HasPrefix(name, "/") {
		return ParsedPath{}, fmt.Errorf("config path %q must be relative to the store", name)
	}
	if name == "~" || strings.HasPrefix(name, "~/") {
		return ParsedPath{}, fmt.Errorf("config path %q cannot use home-relative syntax within a store", name)
	}

	clean := path.Clean(name)
	if clean == "." || clean == "" {
		return ParsedPath{}, fmt.Errorf("config path %q must identify a file", name)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return ParsedPath{}, fmt.Errorf("config path %q escapes the store root", name)
	}

	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return ParsedPath{}, fmt.Errorf("invalid config path %q", name)
		}
	}

	base := parts[len(parts)-1]
	key := base
	ext := path.Ext(base)
	format := ""
	if ext != "" {
		format = strings.TrimPrefix(ext, ".")
		key = strings.TrimSuffix(base, ext)
	}
	if key == "" {
		return ParsedPath{}, fmt.Errorf("config path %q must identify a file", name)
	}
	key = DecodeKey(key)

	namespaces := append([]string(nil), root.Namespaces...)
	namespaces = append(namespaces, parts[:len(parts)-1]...)
	return ParsedPath{
		StoreRoot: StoreRoot{
			AppName:    root.AppName,
			Namespaces: namespaces,
		},
		Descriptor: RequestedFormatKey(key, format),
	}, nil
}

func resolveSimpleParsedPath(parsed ParsedPath, marshaller marshal.FileMarshaller) (ParsedPath, error) {
	if hinted, ok := parsed.Descriptor.(RequestedFormatDescriptor); ok {
		if hinted.Format() != marshaller.Extension() {
			return ParsedPath{}, fmt.Errorf("config path must end in .%s, got .%s", marshaller.Extension(), hinted.Format())
		}
	}
	parsed.Descriptor = Key(parsed.Descriptor.Key())
	return parsed, nil
}

func resolveMultiParsedPath(parsed ParsedPath, marshallers []marshal.FileMarshaller) (ParsedPath, error) {
	if hinted, ok := parsed.Descriptor.(RequestedFormatDescriptor); ok {
		marshaller := marshal.FileMarshallers(marshallers).ByFormat(hinted.Format())
		if marshaller == nil {
			return ParsedPath{}, fmt.Errorf("config path uses unknown extension .%s", hinted.Format())
		}
		parsed.Descriptor = FormatKey(parsed.Descriptor.Key(), marshaller)
		return parsed, nil
	}

	parsed.Descriptor = Key(parsed.Descriptor.Key())
	return parsed, nil
}
