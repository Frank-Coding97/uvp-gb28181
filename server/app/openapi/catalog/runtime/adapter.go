package runtime

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrAdapterRegistryUnavailable = errors.New("OpenAPI adapter registry unavailable")
	ErrInvalidAdapterRegistration = errors.New("invalid OpenAPI adapter registration")
	ErrAdapterAlreadyRegistered   = errors.New("OpenAPI adapter already registered")
	ErrAdapterNotFound            = errors.New("OpenAPI adapter not found")
)

// Invocation is the only request shape an OpenAPI adapter receives. The
// adapter registry does not execute arbitrary URLs, SQL, or Go functions from
// catalog data; a registered adapter is selected by its immutable key and
// contract version.
type Invocation struct {
	Scope  string
	Method string
	Path   string
	Params map[string]string
	Query  map[string]string
	Body   []byte
	Value  any
}

// Adapter contains the business handoff behind one published operation. The
// implementation is normally a thin boundary over an existing application
// service, not a second copy of the business logic.
type Adapter interface {
	Execute(context.Context, Invocation) (any, error)
}

type AdapterRegistration struct {
	Key             string
	ContractVersion string
	Adapter         Adapter
}

type AdapterRef struct {
	Key             string `json:"adapterKey"`
	ContractVersion string `json:"contractVersion"`
}

// AdapterRegistry is deliberately process-local. Database rows can select a
// registered ref, but cannot introduce executable behavior at runtime.
type AdapterRegistry struct {
	mu      sync.RWMutex
	entries map[AdapterRef]Adapter
}

// AdapterBindingSet keeps service-bound adapters replaceable across SIP
// runtime generations while the catalog registry itself stays immutable.
// Forwarders are registered at HTTP startup; they resolve the current domain
// adapter on each call and fail closed when the SIP runtime is unavailable.
type AdapterBindingSet struct {
	mu      sync.RWMutex
	entries map[string]Adapter
}

func NewAdapterBindingSet() *AdapterBindingSet {
	return &AdapterBindingSet{entries: make(map[string]Adapter)}
}

// Bind atomically adds or replaces one generation's adapter registrations.
// Every registration is validated before any binding becomes visible.
func (s *AdapterBindingSet) Bind(registrations []AdapterRegistration) error {
	if s == nil {
		return ErrAdapterRegistryUnavailable
	}
	pending := make(map[string]Adapter, len(registrations))
	for _, registration := range registrations {
		key := strings.TrimSpace(registration.Key)
		version := strings.TrimSpace(registration.ContractVersion)
		if !validToken(key, 64) || !validToken(version, 32) || registration.Adapter == nil {
			return ErrInvalidAdapterRegistration
		}
		if _, exists := pending[key]; exists {
			return ErrAdapterAlreadyRegistered
		}
		pending[key] = registration.Adapter
	}
	s.mu.Lock()
	if s.entries == nil {
		s.entries = make(map[string]Adapter)
	}
	for key, adapter := range pending {
		s.entries[key] = adapter
	}
	s.mu.Unlock()
	return nil
}

func (s *AdapterBindingSet) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.entries = make(map[string]Adapter)
	s.mu.Unlock()
}

// Forwarders returns stable code-owned wrappers for the supplied keys. A
// missing service binding remains a registered but unavailable capability;
// editable catalog data can never supply the implementation.
func (s *AdapterBindingSet) Forwarders(keys []string) map[string]Adapter {
	result := make(map[string]Adapter, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		result[key] = adapterForwarder{set: s, key: key}
	}
	return result
}

func (s *AdapterBindingSet) resolve(key string) Adapter {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	adapter := s.entries[key]
	s.mu.RUnlock()
	return adapter
}

type adapterForwarder struct {
	set *AdapterBindingSet
	key string
}

func (f adapterForwarder) Execute(ctx context.Context, invocation Invocation) (any, error) {
	if f.set == nil {
		return nil, ErrAdapterNotFound
	}
	adapter := f.set.resolve(f.key)
	if adapter == nil {
		return nil, ErrAdapterNotFound
	}
	return adapter.Execute(ctx, invocation)
}

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{entries: make(map[AdapterRef]Adapter)}
}

func (r *AdapterRegistry) Register(registration AdapterRegistration) error {
	if r == nil {
		return ErrAdapterRegistryUnavailable
	}
	key := strings.TrimSpace(registration.Key)
	version := strings.TrimSpace(registration.ContractVersion)
	if !validToken(key, 64) || !validToken(version, 32) || registration.Adapter == nil {
		return ErrInvalidAdapterRegistration
	}
	ref := AdapterRef{Key: key, ContractVersion: version}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[AdapterRef]Adapter)
	}
	if _, exists := r.entries[ref]; exists {
		return ErrAdapterAlreadyRegistered
	}
	r.entries[ref] = registration.Adapter
	return nil
}

func (r *AdapterRegistry) Resolve(key, contractVersion string) (Adapter, error) {
	if r == nil {
		return nil, ErrAdapterRegistryUnavailable
	}
	ref := AdapterRef{Key: strings.TrimSpace(key), ContractVersion: strings.TrimSpace(contractVersion)}
	r.mu.RLock()
	adapter, ok := r.entries[ref]
	r.mu.RUnlock()
	if !ok || adapter == nil {
		return nil, ErrAdapterNotFound
	}
	return adapter, nil
}

func (r *AdapterRegistry) Has(key, contractVersion string) bool {
	_, err := r.Resolve(key, contractVersion)
	return err == nil
}

func (r *AdapterRegistry) Registrations() []AdapterRef {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	refs := make([]AdapterRef, 0, len(r.entries))
	for ref := range r.entries {
		refs = append(refs, ref)
	}
	r.mu.RUnlock()
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Key == refs[j].Key {
			return refs[i].ContractVersion < refs[j].ContractVersion
		}
		return refs[i].Key < refs[j].Key
	})
	return refs
}

func validToken(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (index > 0 && (r == '.' || r == '_' || r == '-')) {
			continue
		}
		return false
	}
	return true
}
