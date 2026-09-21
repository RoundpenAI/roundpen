// Package settingitems stores named, typed configuration entries ("items")
// grouped by kind, plus the bindings recording which item a usage site
// ("slot") resolves to for a given scope.
package settingitems

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/secretbox"
)

// Kind names a family of configurable backends.
type Kind string

const (
	KindLLM     Kind = "llm"
	KindProxy   Kind = "proxy"
	KindSearch  Kind = "search"
	KindBrowser Kind = "browser"
)

var itemIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidItemID reports whether id is usable as a stable, URL- and env-safe id.
func ValidItemID(id string) bool { return itemIDPattern.MatchString(id) }

// SlugifyID derives a valid item id from arbitrary text (spaces and other
// separators collapse to "-", leading/trailing punctuation is dropped).
func SlugifyID(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-._")
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-._")
	}
	return out
}

// FieldType drives generic validation and the settings form rendering.
type FieldType string

const (
	FieldString  FieldType = "string"
	FieldSecret  FieldType = "secret"
	FieldURL     FieldType = "url"
	FieldInt     FieldType = "int"
	FieldBool    FieldType = "bool"
	FieldEnum    FieldType = "enum"
	FieldJSON    FieldType = "json"
	FieldItemRef FieldType = "itemRef"
)

// Field describes one key of an item's config.
type Field struct {
	Key      string    `json:"key"`
	Type     FieldType `json:"type"`
	Label    string    `json:"label,omitempty"`
	Hint     string    `json:"hint,omitempty"`
	Required bool      `json:"required,omitempty"`
	Options  []string  `json:"options,omitempty"` // enum values
	RefKind  Kind      `json:"refKind,omitempty"` // referenced kind for itemRef
	Advanced bool      `json:"advanced,omitempty"`
}

// KindDef describes a kind of configurable backend. The function fields stay
// server-side; the rest is published to the settings UI as JSON.
type KindDef struct {
	Kind       Kind     `json:"kind"`
	Name       string   `json:"name"`
	Fields     []Field  `json:"fields"`
	Selectable []string `json:"selectable,omitempty"` // keys exposed to non-admin callers; empty = all non-secret
	// Normalize applies defaults (protocol, position, id) before validation.
	Normalize func(*Item) error `json:"-"`
	// Validate checks kind-specific rules beyond the generic ones.
	Validate func(*Item) error `json:"-"`
}

// SecretKeys returns config keys that are sealed at rest and masked in responses.
func (d KindDef) SecretKeys() []string {
	out := make([]string, 0, 2)
	for _, f := range d.Fields {
		if f.Type == FieldSecret {
			out = append(out, f.Key)
		}
	}
	return out
}

// Field looks up a field definition by key.
func (d KindDef) Field(key string) (Field, bool) {
	for _, f := range d.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// SelectableKeys returns the config keys safe for non-admin callers. A nil
// Selectable means every non-secret key; a non-nil empty slice means none.
func (d KindDef) SelectableKeys() []string {
	if d.Selectable != nil {
		return d.Selectable
	}
	out := make([]string, 0, len(d.Fields))
	for _, f := range d.Fields {
		if f.Type != FieldSecret {
			out = append(out, f.Key)
		}
	}
	return out
}

// SlotDef declares a usage site that selects one item of a kind.
type SlotDef struct {
	Key           string   `json:"key"`
	Kind          Kind     `json:"kind"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Protocols     []string `json:"protocols,omitempty"`     // restrict items by config.protocol
	BindingFields []Field  `json:"bindingFields,omitempty"` // per-binding params (llm: model)
	UserOverride  bool     `json:"userOverride"`
	RebuildsEnv   bool     `json:"rebuildsEnv,omitempty"`
	DefaultItem   string   `json:"defaultItem,omitempty"`
	// FallbackSlot resolves through another slot when this one is unbound.
	FallbackSlot string `json:"-"`
}

// ErrInvalid marks an error caused by the submitted value rather than by the
// server, so handlers can answer 400 with the message instead of 500.
var ErrInvalid = errors.New("invalid setting item")

// invalidError carries the message to the client while staying detectable.
type invalidError struct{ msg string }

func (e *invalidError) Error() string { return e.msg }

func (e *invalidError) Unwrap() error { return ErrInvalid }

func invalidf(format string, args ...any) error {
	return &invalidError{msg: fmt.Sprintf(format, args...)}
}

// Item is one configured entry of a kind.
type Item struct {
	Kind        Kind              `json:"kind"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Enabled     bool              `json:"enabled"`
	Position    int               `json:"position"`
	Config      map[string]any    `json:"config"`
	Secrets     map[string]string `json:"-"`
	UpdatedAt   time.Time         `json:"updatedAt,omitempty"`
}

// String returns a string config value.
func (i Item) String(key string) string {
	if i.Config == nil {
		return ""
	}
	s, _ := i.Config[key].(string)
	return strings.TrimSpace(s)
}

// Int returns an int config value (JSON numbers decode as float64).
func (i Item) Int(key string) int {
	switch v := i.Config[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// Bool returns a bool config value.
func (i Item) Bool(key string) bool {
	b, _ := i.Config[key].(bool)
	return b
}

// StringMap returns a map[string]string config value (e.g. modelMap).
func (i Item) StringMap(key string) map[string]string {
	raw, ok := i.Config[key].(map[string]any)
	if !ok {
		if m, ok := i.Config[key].(map[string]string); ok {
			return m
		}
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// List returns a []any config value (e.g. modelPatterns).
func (i Item) List(key string) []any {
	list, _ := i.Config[key].([]any)
	return list
}

// Secret returns a decrypted secret value.
func (i Item) Secret(key string) string {
	if i.Secrets == nil {
		return ""
	}
	return strings.TrimSpace(i.Secrets[key])
}

// AdminView returns a copy safe for admin responses: declared config keys are
// kept, secret fields appear as masks, and the Secrets map is dropped.
func (i Item) AdminView(def KindDef) Item {
	out := i
	out.Config = pickFields(i.Config, allFieldKeys(def))
	for _, key := range def.SecretKeys() {
		if v := i.Secret(key); v != "" {
			out.Config[key] = secretbox.SecretMask
		} else if _, ok := out.Config[key]; !ok {
			out.Config[key] = ""
		}
	}
	out.Secrets = nil
	return out
}

// SelectView returns a copy carrying only the kind's selectable keys.
func (i Item) SelectView(def KindDef) Item {
	out := i
	out.Config = pickFields(i.Config, def.SelectableKeys())
	out.Secrets = nil
	return out
}

func allFieldKeys(def KindDef) []string {
	out := make([]string, 0, len(def.Fields))
	for _, f := range def.Fields {
		out = append(out, f.Key)
	}
	return out
}

func pickFields(config map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		if v, ok := config[k]; ok {
			out[k] = v
		}
	}
	return out
}

// MergeSecrets folds submitted masked or empty secrets back to their stored
// values, moves every declared secret out of Config into Secrets, and drops
// undeclared config keys.
func MergeSecrets(def KindDef, next, prev Item) Item {
	out := next
	secrets := make(map[string]string, len(def.SecretKeys()))
	for _, key := range def.SecretKeys() {
		submitted, _ := out.Config[key].(string)
		secrets[key] = secretbox.ResolveValue(strings.TrimSpace(submitted), prev.Secret(key))
	}
	out.Secrets = secrets
	out.Config = pickFields(out.Config, nonSecretKeys(def))
	return out
}

func nonSecretKeys(def KindDef) []string {
	out := make([]string, 0, len(def.Fields))
	for _, f := range def.Fields {
		if f.Type != FieldSecret {
			out = append(out, f.Key)
		}
	}
	return out
}

// NormalizeItem applies generic normalization then the kind's own rules.
func NormalizeItem(def KindDef, it *Item) error {
	if it.Config == nil {
		it.Config = map[string]any{}
	}
	it.ID = strings.ToLower(strings.TrimSpace(it.ID))
	if !ValidItemID(it.ID) {
		it.ID = SlugifyID(it.ID)
	}
	it.Name = strings.TrimSpace(it.Name)
	it.Description = strings.TrimSpace(it.Description)
	for _, f := range def.Fields {
		if s, ok := it.Config[f.Key].(string); ok {
			it.Config[f.Key] = strings.TrimSpace(s)
		}
	}
	if def.Normalize != nil {
		if err := def.Normalize(it); err != nil {
			return invalidf("%s", err)
		}
	}
	return nil
}

// ValidateItem checks generic rules then the kind's own rules.
func ValidateItem(def KindDef, it Item) error {
	if !ValidItemID(it.ID) {
		return invalidf("id %q must be a lowercase slug ([a-z0-9._-], max 64 chars)", it.ID)
	}
	if it.Name == "" {
		return invalidf("name is required")
	}
	for _, f := range def.Fields {
		if !f.Required {
			continue
		}
		if f.Type == FieldSecret {
			if strings.TrimSpace(it.Secrets[f.Key]) == "" {
				return invalidf("%s is required", f.Key)
			}
			continue
		}
		if strings.TrimSpace(fmt.Sprint(it.Config[f.Key])) == "" {
			return invalidf("%s is required", f.Key)
		}
	}
	if def.Validate != nil {
		if err := def.Validate(&it); err != nil {
			return invalidf("%s", err)
		}
	}
	return nil
}

// Binding records the item a slot resolves to for one scope.
type Binding struct {
	Scope  string         `json:"scope"`
	Slot   string         `json:"slot"`
	Kind   Kind           `json:"kind"`
	ItemID string         `json:"itemId"`
	Params map[string]any `json:"params,omitempty"`
}

// UserScope returns the binding scope for a username.
func UserScope(username string) string { return "user:" + username }

// GlobalScope is the admin-defined default scope.
const GlobalScope = "global"

// Param returns a string binding parameter.
func (b Binding) Param(key string) string {
	if b.Params == nil {
		return ""
	}
	s, _ := b.Params[key].(string)
	return strings.TrimSpace(s)
}

// Resolution is the item a slot resolves to for a caller.
type Resolution struct {
	Slot   string
	Kind   Kind
	Item   Item
	Params map[string]any
	Source string // "user" | "global" | "default"
	Scope  string // binding scope that matched; empty for the default item
	OK     bool
}

// Param returns a string parameter of the matched binding.
func (r Resolution) Param(key string) string {
	if r.Params == nil {
		return ""
	}
	s, _ := r.Params[key].(string)
	return strings.TrimSpace(s)
}

// Registry is the code-side catalog of kinds and slots.
type Registry struct {
	kinds     []KindDef
	slots     []SlotDef
	kindIndex map[Kind]KindDef
	slotIndex map[string]SlotDef
}

// NewRegistry validates and indexes kind and slot definitions.
func NewRegistry(kinds []KindDef, slots []SlotDef) (*Registry, error) {
	r := &Registry{
		kinds:     kinds,
		slots:     slots,
		kindIndex: make(map[Kind]KindDef, len(kinds)),
		slotIndex: make(map[string]SlotDef, len(slots)),
	}
	for _, def := range kinds {
		if def.Kind == "" {
			return nil, fmt.Errorf("settingitems: kind name is required")
		}
		if _, dup := r.kindIndex[def.Kind]; dup {
			return nil, fmt.Errorf("settingitems: duplicate kind %q", def.Kind)
		}
		r.kindIndex[def.Kind] = def
	}
	for _, slot := range slots {
		if slot.Key == "" {
			return nil, fmt.Errorf("settingitems: slot key is required")
		}
		if _, ok := r.kindIndex[slot.Kind]; !ok {
			return nil, fmt.Errorf("settingitems: slot %q references unknown kind %q", slot.Key, slot.Kind)
		}
		if _, dup := r.slotIndex[slot.Key]; dup {
			return nil, fmt.Errorf("settingitems: duplicate slot %q", slot.Key)
		}
		if slot.FallbackSlot != "" {
			if _, ok := r.slotIndex[slot.FallbackSlot]; !ok && !hasSlotKey(slots, slot.FallbackSlot) {
				return nil, fmt.Errorf("settingitems: slot %q falls back to unknown slot %q", slot.Key, slot.FallbackSlot)
			}
		}
		r.slotIndex[slot.Key] = slot
	}
	return r, nil
}

func hasSlotKey(slots []SlotDef, key string) bool {
	for _, s := range slots {
		if s.Key == key {
			return true
		}
	}
	return false
}

// Kinds returns the registered kind definitions.
func (r *Registry) Kinds() []KindDef { return r.kinds }

// Kind looks up a kind definition.
func (r *Registry) Kind(k Kind) (KindDef, bool) {
	def, ok := r.kindIndex[k]
	return def, ok
}

// Slots returns the registered slot definitions.
func (r *Registry) Slots() []SlotDef { return r.slots }

// Slot looks up a slot definition.
func (r *Registry) Slot(key string) (SlotDef, bool) {
	def, ok := r.slotIndex[key]
	return def, ok
}
