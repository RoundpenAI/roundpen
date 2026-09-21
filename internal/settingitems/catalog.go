package settingitems

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// ErrKindUnknown reports an unregistered kind.
var ErrKindUnknown = errors.New("unknown kind")

// ErrSlotUnknown reports an unregistered slot.
var ErrSlotUnknown = errors.New("unknown slot")

// BoundError reports that an item is still selected by slots.
type BoundError struct{ Slots []string }

func (e *BoundError) Error() string {
	return fmt.Sprintf("item is still bound to slots: %s", strings.Join(e.Slots, ", "))
}

// Catalog owns the kind registry plus an in-memory snapshot of items and
// bindings, refreshed on every write and periodically so co-located daemons
// sharing one database observe each other.
type Catalog struct {
	store Store
	reg   *Registry

	mu        sync.Mutex
	snap      atomic.Pointer[Snapshot]
	listeners []func()
}

// NewCatalog returns an empty catalog; call Reload to fill it.
func NewCatalog(store Store, reg *Registry) *Catalog {
	c := &Catalog{store: store, reg: reg}
	c.snap.Store(newSnapshot(reg))
	return c
}

// Registry returns the kind and slot definitions.
func (c *Catalog) Registry() *Registry { return c.reg }

// Snapshot returns the current read-only state. Safe for hot paths: a single
// atomic load, no locking and no database round-trip.
func (c *Catalog) Snapshot() *Snapshot { return c.snap.Load() }

// Reload re-reads items and bindings and atomically swaps the snapshot.
func (c *Catalog) Reload(ctx context.Context) error {
	items, err := c.store.List(ctx, "")
	if err != nil {
		return err
	}
	bindings, err := c.store.ListBindings(ctx)
	if err != nil {
		return err
	}
	next := newSnapshot(c.reg)
	for _, it := range items {
		next.items[itemKey{it.Kind, it.ID}] = it
	}
	for _, b := range bindings {
		next.bindings[bindingKey{b.Scope, b.Slot}] = b
	}
	next.rev = c.snap.Load().rev + 1
	next.loadedAt = time.Now()
	c.snap.Store(next)
	c.notify()
	return nil
}

// OnReload registers a listener invoked after every snapshot swap. The
// control plane uses it to push provider projections into the subsystems that
// keep their own in-memory view (the LLM relay, for one).
func (c *Catalog) OnReload(f func()) {
	if f == nil {
		return
	}
	c.mu.Lock()
	c.listeners = append(c.listeners, f)
	c.mu.Unlock()
}

func (c *Catalog) notify() {
	c.mu.Lock()
	listeners := append([]func(){}, c.listeners...)
	c.mu.Unlock()
	for _, f := range listeners {
		f()
	}
}

// RefreshLoop reloads every interval until ctx is done.
func (c *Catalog) RefreshLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Reload(ctx); err != nil {
				continue
			}
		}
	}
}

// Item loads one item straight from storage (admin paths, not hot paths).
func (c *Catalog) Item(ctx context.Context, kind Kind, id string) (Item, error) {
	return c.store.Get(ctx, kind, id)
}

// Save validates and persists an item, then reloads the snapshot. Secrets
// submitted masked or empty keep their stored values; for a new item the
// position defaults to the end of its kind.
func (c *Catalog) Save(ctx context.Context, it Item) (Item, error) {
	def, ok := c.reg.Kind(it.Kind)
	if !ok {
		return Item{}, fmt.Errorf("%w: %q", ErrKindUnknown, it.Kind)
	}
	if err := NormalizeItem(def, &it); err != nil {
		return Item{}, err
	}
	prev, err := c.store.Get(ctx, it.Kind, it.ID)
	switch {
	case err == nil:
	case errors.Is(err, storage.ErrNotFound):
		it.Position = c.nextPosition(it.Kind)
	default:
		return Item{}, err
	}
	it = MergeSecrets(def, it, prev)
	if err := ValidateItem(def, it); err != nil {
		return Item{}, err
	}
	if err := c.store.Upsert(ctx, it); err != nil {
		return Item{}, err
	}
	if err := c.Reload(ctx); err != nil {
		return Item{}, err
	}
	return it, nil
}

// nextPosition puts a new item at the end of its kind's list.
func (c *Catalog) nextPosition(kind Kind) int {
	items := c.snap.Load().Items(kind, true)
	next := 0
	for _, it := range items {
		if it.Position >= next {
			next = it.Position + 1
		}
	}
	return next
}

// Delete removes an item. Unless force is set, an item still selected by a
// binding is refused with a *BoundError.
func (c *Catalog) Delete(ctx context.Context, kind Kind, id string, force bool) error {
	if !force {
		slots, err := c.store.BindingSlotsForItem(ctx, kind, id)
		if err != nil {
			return err
		}
		if len(slots) > 0 {
			return &BoundError{Slots: slots}
		}
	}
	if err := c.store.Delete(ctx, kind, id); err != nil {
		return err
	}
	return c.Reload(ctx)
}

// Reorder persists a new display order for one kind.
func (c *Catalog) Reorder(ctx context.Context, kind Kind, ids []string) error {
	if _, ok := c.reg.Kind(kind); !ok {
		return fmt.Errorf("%w: %q", ErrKindUnknown, kind)
	}
	if err := c.store.Reorder(ctx, kind, ids); err != nil {
		return err
	}
	return c.Reload(ctx)
}

// SetBinding validates and persists a slot selection.
func (c *Catalog) SetBinding(ctx context.Context, b Binding) error {
	def, ok := c.reg.Slot(b.Slot)
	if !ok {
		return fmt.Errorf("%w: %q", ErrSlotUnknown, b.Slot)
	}
	if b.Scope == "" {
		b.Scope = GlobalScope
	}
	if b.Scope != GlobalScope && !strings.HasPrefix(b.Scope, "user:") {
		return fmt.Errorf("scope %q must be %q or user:<username>", b.Scope, GlobalScope)
	}
	if b.Scope != GlobalScope && !def.UserOverride {
		return fmt.Errorf("slot %q cannot be overridden per user", b.Slot)
	}
	if b.ItemID != "" {
		it, err := c.store.Get(ctx, def.Kind, b.ItemID)
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("unknown %s item %q", def.Kind, b.ItemID)
		}
		if err != nil {
			return err
		}
		if !it.Enabled {
			return fmt.Errorf("%s item %q is disabled", def.Kind, b.ItemID)
		}
		if len(def.Protocols) > 0 && !containsString(def.Protocols, it.String("protocol")) {
			return fmt.Errorf("%s item %q does not support this slot", def.Kind, b.ItemID)
		}
	}
	b.Kind = def.Kind
	b.Params = pickParams(def, b.Params)
	if err := c.store.SetBinding(ctx, b); err != nil {
		return err
	}
	return c.Reload(ctx)
}

// DeleteBinding clears a slot selection for one scope.
func (c *Catalog) DeleteBinding(ctx context.Context, scope, slot string) error {
	if _, ok := c.reg.Slot(slot); !ok {
		return fmt.Errorf("%w: %q", ErrSlotUnknown, slot)
	}
	if err := c.store.DeleteBinding(ctx, scope, slot); err != nil {
		return err
	}
	return c.Reload(ctx)
}

func pickParams(def SlotDef, params map[string]any) map[string]any {
	if len(params) == 0 || len(def.BindingFields) == 0 {
		return nil
	}
	out := make(map[string]any, len(def.BindingFields))
	for _, f := range def.BindingFields {
		v, ok := params[f.Key]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		out[f.Key] = strings.TrimSpace(s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type itemKey struct {
	kind Kind
	id   string
}

type bindingKey struct {
	scope string
	slot  string
}

// Snapshot is a consistent read-only view of items and bindings.
type Snapshot struct {
	registry *Registry
	items    map[itemKey]Item
	bindings map[bindingKey]Binding
	rev      int64
	loadedAt time.Time
}

func newSnapshot(reg *Registry) *Snapshot {
	return &Snapshot{
		registry: reg,
		items:    map[itemKey]Item{},
		bindings: map[bindingKey]Binding{},
	}
}

// Rev is the snapshot revision, incremented on every reload.
func (s *Snapshot) Rev() int64 { return s.rev }

// Item returns an enabled item by kind and id.
func (s *Snapshot) Item(kind Kind, id string) (Item, bool) {
	it, ok := s.items[itemKey{kind, id}]
	if !ok || !it.Enabled {
		return Item{}, false
	}
	return it, true
}

// Items returns a kind's items ordered by position, disabled ones included
// when all is set.
func (s *Snapshot) Items(kind Kind, all bool) []Item {
	out := make([]Item, 0, len(s.items))
	for key, it := range s.items {
		if key.kind != kind {
			continue
		}
		if !it.Enabled && !all {
			continue
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Binding returns the stored selection for a scope and slot.
func (s *Snapshot) Binding(scope, slot string) (Binding, bool) {
	b, ok := s.bindings[bindingKey{scope, slot}]
	return b, ok
}

// Resolve returns the item a slot resolves to for user ("" = anonymous):
// user override, then the global default, then the slot's default item, then
// the slot's fallback slot. Every hop is skipped when its item is missing,
// disabled, or of the wrong kind or protocol, so resolution never fails hard.
func (s *Snapshot) Resolve(slot, user string) Resolution {
	def, ok := s.registry.Slot(slot)
	if !ok {
		return Resolution{Slot: slot}
	}
	if res, ok := s.resolveChain(def, user, true); ok {
		return res
	}
	return Resolution{Slot: slot, Kind: def.Kind}
}

func (s *Snapshot) resolveChain(def SlotDef, user string, allowFallback bool) (Resolution, bool) {
	if def.UserOverride && user != "" {
		if res, ok := s.fromScope(def, UserScope(user), "user"); ok {
			return res, true
		}
	}
	if res, ok := s.fromScope(def, GlobalScope, "global"); ok {
		return res, true
	}
	if def.DefaultItem != "" {
		if it, ok := s.usable(def, def.DefaultItem); ok {
			return Resolution{Slot: def.Key, Kind: def.Kind, Item: it, Source: "default", OK: true}, true
		}
	}
	if allowFallback && def.FallbackSlot != "" {
		if fb, ok := s.registry.Slot(def.FallbackSlot); ok && fb.Key != def.Key {
			if res, ok := s.resolveChain(fb, user, false); ok {
				res.Slot = def.Key
				res.Source = "fallback"
				return res, true
			}
		}
	}
	return Resolution{}, false
}

func (s *Snapshot) fromScope(def SlotDef, scope, source string) (Resolution, bool) {
	b, ok := s.bindings[bindingKey{scope, def.Key}]
	if !ok || b.ItemID == "" {
		return Resolution{}, false
	}
	it, ok := s.usable(def, b.ItemID)
	if !ok {
		return Resolution{}, false
	}
	return Resolution{
		Slot:   def.Key,
		Kind:   def.Kind,
		Item:   it,
		Params: b.Params,
		Source: source,
		Scope:  scope,
		OK:     true,
	}, true
}

func (s *Snapshot) usable(def SlotDef, id string) (Item, bool) {
	it, ok := s.items[itemKey{def.Kind, id}]
	if !ok || !it.Enabled {
		return Item{}, false
	}
	if len(def.Protocols) > 0 && !containsString(def.Protocols, it.String("protocol")) {
		return Item{}, false
	}
	return it, true
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
