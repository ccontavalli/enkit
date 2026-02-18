package config

import "sort"

// SortedKeys returns a sorted copy of the keys slice.
func SortedKeys(keys []string) []string {
	out := append([]string(nil), keys...)
	sort.Strings(out)
	return out
}

// KeysFromSet returns keys from a set.
func KeysFromSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	return keys
}

// DescriptorsFromKeys converts keys to Key descriptors.
func DescriptorsFromKeys(keys []string) []Descriptor {
	descs := make([]Descriptor, len(keys))
	for i, key := range keys {
		descs[i] = Key(key)
	}
	return descs
}

// SortedDescriptorsFromKeys returns sorted descriptors from keys.
func SortedDescriptorsFromKeys(keys []string) []Descriptor {
	return DescriptorsFromKeys(SortedKeys(keys))
}
