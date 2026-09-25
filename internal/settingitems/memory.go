package settingitems

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// MemoryStore is an in-memory Store for tests and the UI smoke backend. It
// applies the same sealing rules as PgStore so callers behave identically.
type MemoryStore struct {
	mu       sync.Mutex
	box      *secretbox.Box
	items    map[itemKey]Item
	bindings map[bindingKey]Binding
}

// NewMemoryStore returns an empty in-memory store; a nil box disables sealing.
func NewMemoryStore(box *secretbox.Box) *MemoryStore {
	return &MemoryStore{
		box:      box,
		items:    map[itemKey]Item{},
		bindings: map[bindingKey]Binding{},
	}
}

func (m *MemoryStore) Count(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items), nil
}

func (m *MemoryStore) List(_ context.Context, kind Kind) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Item{}
	for key, it := range m.items {
		if kind != "" && key.kind != kind {
			continue
		}
		out = append(out, m.clone(it))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *MemoryStore) Get(_ context.Context, kind Kind, id string) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[itemKey{kind, id}]
	if !ok {
		return Item{}, storage.ErrNotFound
	}
	return m.clone(it), nil
}

func (m *MemoryStore) Upsert(_ context.Context, it Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[itemKey{it.Kind, it.ID}] = m.clone(it)
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, kind Kind, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, itemKey{kind, id})
	for key, b := range m.bindings {
		if b.Kind == kind && b.ItemID == id {
			delete(m.bindings, key)
		}
	}
	return nil
}

func (m *MemoryStore) Reorder(_ context.Context, kind Kind, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, id := range ids {
		key := itemKey{kind, id}
		it, ok := m.items[key]
		if !ok {
			continue
		}
		it.Position = i
		it.UpdatedAt = time.Now().UTC()
		m.items[key] = it
	}
	return nil
}

func (m *MemoryStore) ListBindings(_ context.Context) ([]Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Binding{}
	for _, b := range m.bindings {
		out = append(out, cloneBinding(b))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Slot < out[j].Slot
	})
	return out, nil
}

func (m *MemoryStore) SetBinding(_ context.Context, b Binding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bindings[bindingKey{b.Scope, b.Slot}] = cloneBinding(b)
	return nil
}

func (m *MemoryStore) DeleteBinding(_ context.Context, scope, slot string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.bindings, bindingKey{scope, slot})
	return nil
}

func (m *MemoryStore) BindingSlotsForItem(_ context.Context, kind Kind, id string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]struct{}{}
	out := []string{}
	for _, b := range m.bindings {
		if b.Kind != kind || b.ItemID != id {
			continue
		}
		if _, dup := seen[b.Slot]; dup {
			continue
		}
		seen[b.Slot] = struct{}{}
		out = append(out, b.Slot)
	}
	sort.Strings(out)
	return out, nil
}

// clone copies an item and seals or opens its secrets the way PgStore would.
func (m *MemoryStore) clone(it Item) Item {
	out := it
	out.Config = make(map[string]any, len(it.Config))
	for k, v := range it.Config {
		out.Config[k] = v
	}
	out.Secrets = make(map[string]string, len(it.Secrets))
	for k, v := range it.Secrets {
		out.Secrets[k] = m.transform(it.Kind, it.ID, k, v)
	}
	return out
}

// transform is a no-op for a nil box; otherwise it seals when the value is a
// plaintext secret and opens when it is sealed, which keeps the in-memory
// store's behavior identical to the database one.
func (m *MemoryStore) transform(kind Kind, id, key, v string) string {
	if m.box == nil {
		return v
	}
	if secretbox.IsSealed(v) {
		plain, err := m.box.Open(v)
		if err != nil {
			slog.Warn("settingitems: secret decryption failed; blanking field",
				"kind", string(kind), "id", id, "field", key, "err", err)
			return ""
		}
		return plain
	}
	sealed, err := m.box.Seal(v)
	if err != nil {
		slog.Warn("settingitems: secret sealing failed", "kind", string(kind), "id", id, "field", key, "err", err)
		return v
	}
	return sealed
}

func cloneBinding(b Binding) Binding {
	out := b
	if b.Params != nil {
		out.Params = make(map[string]any, len(b.Params))
		for k, v := range b.Params {
			out.Params[k] = v
		}
	}
	return out
}
