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
		out[i] = Key(s.codec.Decode(desc.Key()))
	}
	return out, nil
}

func (s *CodecStore) DownloadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error) {
	return s.store.DownloadURL(Key(s.codec.Encode(desc.Key())), opts...)
}

func (s *CodecStore) UploadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error) {
	return s.store.UploadURL(Key(s.codec.Encode(desc.Key())), opts...)
}

func (s *CodecStore) Delete(desc Descriptor) error {
	return s.store.Delete(Key(s.codec.Encode(desc.Key())))
}

func (s *CodecStore) Close() error {
	return s.store.Close()
}

var _ Store = (*CodecStore)(nil)
