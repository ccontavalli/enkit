// Package datastore provides a Store capable of storing configs on datastore.
//
// The entry point is the New function, that allows you to create a Datastore
// object.
//
// The Datastore object can then be used to open as many config stores as
// necessary via the Open function, that can be used anywhere a store.Opener
// is accepted.
//
// The Open function returns a proper Store, implementing the Marshal, Unmarshal,
// and Delete methods.
//
// For example:
//
//	ds, err := datastore.New()
//	if err != nil {
//	  ...
//	}
//
//	ids, err := ds.Open("myapp1", "identities")
//	if err != nil { ...
//
//	err, _ := ids.Marshal("carlo@enfabrica.net", credentials)
//	if err != nil { ...
//
//	err, _ := ids.Marshal("default", credentials)
//	if err != nil { ...
//
//	epts, err := ds.Open("myapp1", "endpoints")
//	if err != nil { ...
//
//	err, _ := epts.Marshal("server1", parameters)
//	err, _ := epts.Marshal("server2", parameters)
//
// There are two main optional parameters that can be passed to datastore.New:
// a ContextGenerator, and a KeyInitializer.
//
// A ContextGenerator returns a new context.Context every time it is invoked.
// It can be used to set timeouts for operations, implement cancellations, or simply
// change the context used at run time.
//
// A KeyInitializer generates a datastore.Key based on the parameters passed
// to Open and Marshal. It can be used to map Marshal and Unmarshal operations
// to arbitrary objects in the datastore tree.
//
// To pass google options, a KeyInitializer, or ContextGenerator, you can use
// one of the functional setters with datastore.New(). For example:
//
//	ds, err := datastore.New(WithGoogleOptions(opts), WithKeyInitializer(myfunc), WithContextGenerator(mybar))
package datastore

import (
	"cloud.google.com/go/datastore"
	"context"
	"fmt"
	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/kflags"
	"google.golang.org/api/option"
	"os"
	"reflect"
)

// ContextGenerator is a function capable of initializing or generating a context.
type ContextGenerator func() context.Context

// KeyGenerator is a function capable of generating a datastore key from a Marshal key.
type KeyGenerator func(key string) (*datastore.Key, error)

// KeyInitializer is a function capable of creating a KeyGenerator from Opener parameters.
type KeyInitializer func(app string, namespaces ...string) (KeyGenerator, error)

type Datastore struct {
	Client          *datastore.Client
	InitializeKey   KeyInitializer
	GenerateContext ContextGenerator
}

// Close releases any resources owned by the datastore client.
func (ds *Datastore) Close() error {
	if ds.Client == nil {
		return nil
	}
	return ds.Client.Close()
}

var KindApp = "app"
var KindNs = "ns"
var KindEl = "el"

// DefaultKeyGenerator generates a key by appending "el=key" to the specified root.
var DefaultKeyGenerator = func(root *datastore.Key) KeyGenerator {
	return func(element string) (*datastore.Key, error) {
		return datastore.NameKey(KindEl, element, root), nil
	}
}

// DefaultKeyInitializer returns a KeyGenerator using "app=myapp,ns=namespace,ns=namespace,..."
// as root based on Open() parameters.
var DefaultKeyInitializer KeyInitializer = func(app string, namespaces ...string) (KeyGenerator, error) {
	root := datastore.NameKey(KindApp, app, nil)
	for _, ns := range namespaces {
		root = datastore.NameKey(KindNs, ns, root)
	}
	return DefaultKeyGenerator(root), nil
}

type options struct {
	project   string
	dsoptions []option.ClientOption

	initializer KeyInitializer
	cgenerator  ContextGenerator
}

type Modifier func(opt *options) error
type Modifiers []Modifier

// Flags holds configuration options for datastore stores.
type Flags struct {
	// Project specifies the Google Cloud Project ID.
	Project string
}

// DefaultFlags returns a new Flags struct with default values.
func DefaultFlags() *Flags {
	return &Flags{}
}

// Register registers the datastore flags with the provided FlagSet.
func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.StringVar(&f.Project, prefix+"config-store-datastore-project", f.Project, "Project ID for Datastore config backend (optional, defaults to auto-detect)")
	return f
}

// FromFlags returns a Modifier that applies datastore flags.
func FromFlags(flags *Flags) Modifier {
	return func(opt *options) error {
		if flags == nil {
			return nil
		}
		if flags.Project != "" {
			opt.project = flags.Project
		}
		return nil
	}
}

// WithProject specifies the datastore project name.
func WithProject(project string) Modifier {
	return func(opt *options) error {
		opt.project = project
		return nil
	}
}

func WithKeyInitializer(ki KeyInitializer) Modifier {
	return func(opt *options) error {
		opt.initializer = ki
		return nil
	}
}

func WithGoogleOptions(option ...option.ClientOption) Modifier {
	return func(opt *options) error {
		opt.dsoptions = append(opt.dsoptions, option...)
		return nil
	}
}

func WithContextGenerator(cgenerator ContextGenerator) Modifier {
	return func(opt *options) error {
		opt.cgenerator = cgenerator
		return nil
	}
}

func (mods Modifiers) Apply(opt *options) error {
	for _, m := range mods {
		if err := m(opt); err != nil {
			return err
		}
	}
	return nil
}

// DefaultContextGenerator returns a context with no deadline and no cancellation.
var DefaultContextGenerator ContextGenerator = func() context.Context {
	return context.Background()
}

func New(mods ...Modifier) (*Datastore, error) {
	opts := options{initializer: DefaultKeyInitializer, cgenerator: DefaultContextGenerator, project: datastore.DetectProjectID}
	if err := Modifiers(mods).Apply(&opts); err != nil {
		return nil, err
	}

	client, err := datastore.NewClient(opts.cgenerator(), opts.project, opts.dsoptions...)
	if err != nil {
		return nil, err
	}

	return &Datastore{
		Client:          client,
		InitializeKey:   opts.initializer,
		GenerateContext: opts.cgenerator,
	}, nil
}

func (ds *Datastore) Open(app string, namespaces ...string) (config.Store, error) {
	generator, err := ds.InitializeKey(app, namespaces...)
	if err != nil {
		return nil, err
	}

	return &Storer{Parent: ds, GenerateKey: generator, GenerateContext: ds.GenerateContext}, nil
}

// Explore returns a store that lists child namespaces under the provided path.
func (ds *Datastore) Explore(app string, namespaces ...string) (config.Explorer, error) {
	generator, err := ds.InitializeKey(app, namespaces...)
	if err != nil {
		return nil, err
	}
	return &explorator{
		parent:          ds,
		app:             app,
		base:            append([]string(nil), namespaces...),
		GenerateKey:     generator,
		GenerateContext: ds.GenerateContext,
	}, nil
}

type explorator struct {
	parent          *Datastore
	app             string
	base            []string
	GenerateKey     KeyGenerator
	GenerateContext ContextGenerator
}

func (s *explorator) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	if opts.Unmarshal != nil {
		return nil, fmt.Errorf("namespace list does not support unmarshal")
	}

	baseKey, err := s.GenerateKey("")
	if err != nil {
		return nil, err
	}
	if baseKey.Parent == nil {
		return nil, fmt.Errorf("namespace base key missing parent")
	}

	q := datastore.NewQuery(baseKey.Kind).Ancestor(baseKey.Parent).KeysOnly()
	keys, err := s.parent.Client.GetAll(s.GenerateContext(), q, nil)
	if err != nil {
		return nil, err
	}

	childSet := map[string]struct{}{}
	for _, key := range keys {
		if child := namespaceChild(baseKey.Parent, key); child != "" {
			childSet[child] = struct{}{}
		}
	}

	descs := config.SortedNamespaceDescriptors(s.base, config.KeysFromSet(childSet))
	return opts.Apply(descs, 0), nil
}

func (s *explorator) Delete(desc config.Descriptor) error {
	path := config.NamespacePathFromDescriptor(s.base, desc)
	generator, err := s.parent.InitializeKey(s.app, path...)
	if err != nil {
		return err
	}
	targetKey, err := generator("")
	if err != nil {
		return err
	}
	if targetKey.Parent == nil {
		return fmt.Errorf("namespace key missing parent")
	}

	q := datastore.NewQuery(targetKey.Kind).Ancestor(targetKey.Parent).KeysOnly()
	keys, err := s.parent.Client.GetAll(s.GenerateContext(), q, nil)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return os.ErrNotExist
	}
	return s.parent.Client.DeleteMulti(s.GenerateContext(), keys)
}

func (s *explorator) Close() error { return nil }

func namespaceChild(base *datastore.Key, key *datastore.Key) string {
	parent := key.Parent
	if parent == nil {
		return ""
	}
	path := make([]*datastore.Key, 0, 4)
	for k := parent; k != nil; k = k.Parent {
		path = append(path, k)
		if datastoreKeyEqual(k, base) {
			break
		}
	}
	if len(path) == 0 || !datastoreKeyEqual(path[len(path)-1], base) {
		return ""
	}
	if len(path) < 2 {
		return ""
	}
	return datastoreKeyString(path[len(path)-2])
}

func datastoreKeyString(key *datastore.Key) string {
	if key.Name != "" {
		return key.Name
	}
	return fmt.Sprintf("%d", key.ID)
}

func datastoreKeyEqual(a, b *datastore.Key) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Kind != b.Kind || a.Name != b.Name || a.ID != b.ID || a.Namespace != b.Namespace {
		return false
	}
	return datastoreKeyEqual(a.Parent, b.Parent)
}

type Storer struct {
	Parent          *Datastore
	GenerateKey     KeyGenerator
	GenerateContext ContextGenerator
}

func (s *Storer) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	key, err := s.GenerateKey("")
	if err != nil {
		return nil, err
	}

	q := datastore.NewQuery(key.Kind).Ancestor(key.Parent)
	if opts.StartFrom != "" {
		startKey, err := s.GenerateKey(opts.StartFrom)
		if err != nil {
			return nil, err
		}
		q = q.Filter("__key__ >=", startKey)
	}
	if opts.Offset > 0 {
		q = q.Offset(opts.Offset)
	}
	if opts.Limit > 0 {
		q = q.Limit(opts.Limit)
	}

	if opts.Unmarshal != nil {
		slicePtr := opts.Unmarshal.NewSlice()
		keys, err := s.Parent.Client.GetAll(s.GenerateContext(), q, slicePtr)
		if err != nil {
			return nil, err
		}
		for i, key := range keys {
			item := opts.Unmarshal.SliceItem(slicePtr, i)
			if err := opts.Unmarshal.Call(config.Key(key.Name), item); err != nil {
				return nil, err
			}
		}
		return opts.Finalize(s, []config.Descriptor{}, config.OptimizedStartFrom|config.OptimizedOffsetLimit|config.OptimizedUnmarshal)
	}

	keys, err := s.Parent.Client.GetAll(s.GenerateContext(), q.KeysOnly(), nil)
	if err != nil {
		return nil, err
	}

	result := []config.Descriptor{}
	for _, key := range keys {
		result = append(result, config.Key(key.Name))
	}
	return opts.Finalize(s, result, config.OptimizedStartFrom|config.OptimizedOffsetLimit|config.OptimizedUnmarshal)
}
func (s *Storer) Marshal(descriptor config.Descriptor, value interface{}) error {
	if reflect.ValueOf(value).Kind() != reflect.Ptr {
		vp := reflect.New(reflect.TypeOf(value))
		vp.Elem().Set(reflect.ValueOf(value))
		value = vp.Interface()
	}

	if descriptor == nil {
		return fmt.Errorf("invalid key: <nil>")
	}
	name := descriptor.Key()
	key, err := s.GenerateKey(name)
	if err != nil {
		return err
	}

	if _, err := s.Parent.Client.Put(s.GenerateContext(), key, value); err != nil {
		return err
	}
	return nil
}

func (s *Storer) Unmarshal(desc config.Descriptor, value interface{}) (config.Descriptor, error) {
	if desc == nil {
		return nil, fmt.Errorf("invalid key: <nil>")
	}
	name := desc.Key()
	key, err := s.GenerateKey(name)
	if err != nil {
		return nil, err
	}

	if err := s.Parent.Client.Get(s.GenerateContext(), key, value); err != nil {
		if err == datastore.ErrNoSuchEntity {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return config.Key(name), nil
}

func (s *Storer) Delete(descriptor config.Descriptor) error {
	if descriptor == nil {
		return fmt.Errorf("invalid key: <nil>")
	}
	name := descriptor.Key()
	key, err := s.GenerateKey(name)
	if err != nil {
		return err
	}

	if err := s.Parent.Client.Delete(s.GenerateContext(), key); err != nil {
		return err
	}

	return nil
}

func (s *Storer) Close() error {
	return nil
}
