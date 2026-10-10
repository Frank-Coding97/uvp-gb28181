package logging

import (
	"strings"
	"sync/atomic"
)

// Entries are immutable; only key classification is cached. Field values,
// types, identities and entry budgets are always checked for each write.
type fieldKeyMetadata struct {
	key       string
	sensitive bool
	rule      stringFieldRule
}
type fieldKeyCache struct {
	slots [256]atomic.Pointer[fieldKeyMetadata]
}

func fieldKeySlot(key string) uint32 {
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash = (hash ^ uint32(key[i])) * 16777619
	}
	return hash % 256
}
func (c *fieldKeyCache) lookup(key string) fieldKeyMetadata {
	if c == nil || len(key) > 128 {
		return fieldKeyMetadata{key: key, sensitive: sensitiveKey(key), rule: stringFieldPolicy(key)}
	}
	slot := &c.slots[fieldKeySlot(key)]
	if hit := slot.Load(); hit != nil && hit.key == key {
		return *hit
	}
	value := fieldKeyMetadata{key: strings.Clone(key), sensitive: sensitiveKey(key), rule: stringFieldPolicy(key)}
	slot.Store(&value)
	return value
}
