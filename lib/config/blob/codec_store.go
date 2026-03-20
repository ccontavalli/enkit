package blob

import (
	"fmt"
	"net/url"

	"github.com/ccontavalli/enkit/lib/config"
)

// CodecStore wraps a Store and encodes/decodes descriptor keys.
type CodecStore struct {
	store Store
	codec config.KeyCodec
}

type codecStoreOptions struct {
	codec config.KeyCodec
}

type CodecStoreOption func(*codecStoreOptions) error

type CodecStoreOptions []CodecStoreOption

func (opts CodecStoreOptions) Apply(target *codecStoreOptions) error {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(target); err != nil {
			return err
		}
	}
	return nil
}

// WithKeyCodec specifies the codec to apply to descriptor keys.
func WithKeyCodec(codec config.KeyCodec) CodecStoreOption {
	return func(o *codecStoreOptions) error {
		o.codec = codec
		return nil
	}
}

// NewCodecStore wraps a Store with a key codec.
func NewCodecStore(store Store, mods ...CodecStoreOption) (Store, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	opts := codecStoreOptions{}
	if err := CodecStoreOptions(mods).Apply(&opts); err != nil {
		return nil, err
	}
	if opts.codec == nil {
		return nil, fmt.Errorf("codec is required")
	}
	return &CodecStore{store: store, codec: opts.codec}, nil
}

// WrapCodecStore wraps a Store with the provided key codec.
func WrapCodecStore(store Store, codec config.KeyCodec) Store {
	return &CodecStore{store: store, codec: codec}
}

func (s *CodecStore) List() ([]Descriptor, error) {
	descs, err := s.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]Descriptor, len(descs))
	for i, desc := range descs {
		key, err := s.codec.Decode(desc.Key())
		if err != nil {
			return nil, err
		}
		out[i] = Key(key)
	}
	return out, nil
}

func (s *CodecStore) DownloadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error) {
	key, err := s.codec.Encode(desc.Key())
	if err != nil {
		return nil, err
	}
	return s.store.DownloadURL(Key(key), opts...)
}

func (s *CodecStore) UploadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error) {
	key, err := s.codec.Encode(desc.Key())
	if err != nil {
		return nil, err
	}
	return s.store.UploadURL(Key(key), opts...)
}

func (s *CodecStore) Delete(desc Descriptor) error {
	key, err := s.codec.Encode(desc.Key())
	if err != nil {
		return err
	}
	return s.store.Delete(Key(key))
}

func (s *CodecStore) Close() error {
	return s.store.Close()
}

var _ Store = (*CodecStore)(nil)
