package config

type Binding interface {
	Marshal(value interface{}) error
	Unmarshal(value interface{}) error
}

type StoreBinding struct {
	store Store
	key   string
}

func Bind(store Store, key string) *StoreBinding {
	return &StoreBinding{store: store, key: key}
}

func (b *StoreBinding) Marshal(value interface{}) error {
	return b.store.Marshal(Key(b.key), value)
}

func (b *StoreBinding) Unmarshal(value interface{}) error {
	_, err := b.store.Unmarshal(b.key, value)
	return err
}
