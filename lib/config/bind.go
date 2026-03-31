package config

type Binding interface {
	Marshal(value interface{}) error
	Unmarshal(value interface{}) error
}

type StoreBinding struct {
	store Store
	desc  Descriptor
}

func Bind(store Store, desc Descriptor) *StoreBinding {
	return &StoreBinding{store: store, desc: desc}
}

func (b *StoreBinding) Marshal(value interface{}) error {
	return b.store.Marshal(b.desc, value)
}

func (b *StoreBinding) Unmarshal(value interface{}) error {
	_, err := b.store.Unmarshal(b.desc, value)
	return err
}
