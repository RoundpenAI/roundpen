package settingitems

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Store persists items and bindings.
type Store interface {
	Count(ctx context.Context) (int, error)
	List(ctx context.Context, kind Kind) ([]Item, error)
	Get(ctx context.Context, kind Kind, id string) (Item, error)
	Upsert(ctx context.Context, it Item) error
	Delete(ctx context.Context, kind Kind, id string) error
	Reorder(ctx context.Context, kind Kind, ids []string) error
	ListBindings(ctx context.Context) ([]Binding, error)
	SetBinding(ctx context.Context, b Binding) error
	DeleteBinding(ctx context.Context, scope, slot string) error
	BindingSlotsForItem(ctx context.Context, kind Kind, id string) ([]string, error)
}

// PgStore is the PostgreSQL Store. Secret values are sealed at the storage
// boundary (AES-GCM, "enc:v1:" prefix) and opened on read; a nil box disables
// encryption (tests, single-user dev).
type PgStore struct {
	sql *sql.DB
	box *secretbox.Box
}

// NewPGStore returns an items store over db.
func NewPGStore(db *sql.DB, box *secretbox.Box) *PgStore {
	return &PgStore{sql: db, box: box}
}

const itemCols = `kind, id, name, description, enabled, position, config, secrets, updated_at`

// Count returns the number of stored items across all kinds.
func (s *PgStore) Count(ctx context.Context) (int, error) {
	var n int
	err := s.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM setting_items`).Scan(&n)
	return n, err
}

// List returns items ordered by position, optionally filtered to one kind.
func (s *PgStore) List(ctx context.Context, kind Kind) ([]Item, error) {
	query := `SELECT ` + itemCols + ` FROM setting_items`
	args := []any{}
	if kind != "" {
		query += ` WHERE kind=$1`
		args = append(args, string(kind))
	}
	query += ` ORDER BY kind, position, id`
	rows, err := s.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Get loads one item.
func (s *PgStore) Get(ctx context.Context, kind Kind, id string) (Item, error) {
	row := s.sql.QueryRowContext(ctx, `SELECT `+itemCols+` FROM setting_items WHERE kind=$1 AND id=$2`, string(kind), id)
	it, err := s.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, storage.ErrNotFound
	}
	return it, err
}

// Upsert inserts or replaces an item.
func (s *PgStore) Upsert(ctx context.Context, it Item) error {
	config, err := marshalJSON(it.Config)
	if err != nil {
		return err
	}
	secrets, err := s.sealSecrets(it.Secrets)
	if err != nil {
		return err
	}
	_, err = s.sql.ExecContext(ctx, `
		INSERT INTO setting_items (kind, id, name, description, enabled, position, config, secrets, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (kind, id) DO UPDATE SET
			name=EXCLUDED.name,
			description=EXCLUDED.description,
			enabled=EXCLUDED.enabled,
			position=EXCLUDED.position,
			config=EXCLUDED.config,
			secrets=EXCLUDED.secrets,
			updated_at=now()`,
		string(it.Kind), it.ID, it.Name, it.Description, it.Enabled, it.Position, config, secrets,
	)
	return err
}

// Delete removes an item and every binding that referenced it.
func (s *PgStore) Delete(ctx context.Context, kind Kind, id string) error {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM setting_bindings WHERE kind=$1 AND item_id=$2`, string(kind), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM setting_items WHERE kind=$1 AND id=$2`, string(kind), id); err != nil {
		return err
	}
	return tx.Commit()
}

// Reorder assigns positions following the given id order.
func (s *PgStore) Reorder(ctx context.Context, kind Kind, ids []string) error {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE setting_items SET position=$3, updated_at=now() WHERE kind=$1 AND id=$2`,
			string(kind), id, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListBindings returns every binding across scopes.
func (s *PgStore) ListBindings(ctx context.Context) ([]Binding, error) {
	rows, err := s.sql.QueryContext(ctx,
		`SELECT scope, slot, kind, item_id, params FROM setting_bindings ORDER BY scope, slot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Binding{}
	for rows.Next() {
		var b Binding
		var kind, raw string
		if err := rows.Scan(&b.Scope, &b.Slot, &kind, &b.ItemID, &raw); err != nil {
			return nil, err
		}
		b.Kind = Kind(kind)
		params := map[string]any{}
		if err := unmarshalJSON(raw, &params); err != nil {
			return nil, err
		}
		if len(params) > 0 {
			b.Params = params
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetBinding upserts the item a slot resolves to for one scope.
func (s *PgStore) SetBinding(ctx context.Context, b Binding) error {
	params, err := marshalJSON(b.Params)
	if err != nil {
		return err
	}
	_, err = s.sql.ExecContext(ctx, `
		INSERT INTO setting_bindings (scope, slot, kind, item_id, params, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (scope, slot) DO UPDATE SET
			kind=EXCLUDED.kind,
			item_id=EXCLUDED.item_id,
			params=EXCLUDED.params,
			updated_at=now()`,
		b.Scope, b.Slot, string(b.Kind), b.ItemID, params,
	)
	return err
}

// DeleteBinding clears a scope's selection for a slot.
func (s *PgStore) DeleteBinding(ctx context.Context, scope, slot string) error {
	_, err := s.sql.ExecContext(ctx,
		`DELETE FROM setting_bindings WHERE scope=$1 AND slot=$2`, scope, slot)
	return err
}

// BindingSlotsForItem lists slots bound to an item, in any scope.
func (s *PgStore) BindingSlotsForItem(ctx context.Context, kind Kind, id string) ([]string, error) {
	rows, err := s.sql.QueryContext(ctx,
		`SELECT DISTINCT slot FROM setting_bindings WHERE kind=$1 AND item_id=$2 ORDER BY slot`,
		string(kind), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var slot string
		if err := rows.Scan(&slot); err != nil {
			return nil, err
		}
		out = append(out, slot)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *PgStore) scan(row rowScanner) (Item, error) {
	var it Item
	var kind, config, secrets string
	if err := row.Scan(&kind, &it.ID, &it.Name, &it.Description, &it.Enabled, &it.Position,
		&config, &secrets, &it.UpdatedAt); err != nil {
		return Item{}, err
	}
	it.Kind = Kind(kind)
	if err := unmarshalJSON(config, &it.Config); err != nil {
		return Item{}, fmt.Errorf("setting_items %s/%s: config: %w", kind, it.ID, err)
	}
	raw := map[string]string{}
	if err := unmarshalJSON(secrets, &raw); err != nil {
		return Item{}, fmt.Errorf("setting_items %s/%s: secrets: %w", kind, it.ID, err)
	}
	it.Secrets = s.openSecrets(it.Kind, it.ID, raw)
	return it, nil
}

func marshalJSON(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalJSON(raw string, out any) error {
	if raw == "" {
		raw = "{}"
	}
	return json.Unmarshal([]byte(raw), out)
}

func (s *PgStore) sealSecrets(secrets map[string]string) (string, error) {
	sealed := make(map[string]string, len(secrets))
	for k, v := range secrets {
		if s.box == nil {
			sealed[k] = v
			continue
		}
		out, err := s.box.Seal(v)
		if err != nil {
			return "", err
		}
		sealed[k] = out
	}
	return marshalJSON(sealed)
}

// openSecrets decrypts in place. A value that fails to decrypt (e.g. the
// master key was regenerated) is blanked with a warning so the rest of the
// item stays loadable; the admin re-enters the affected secret.
func (s *PgStore) openSecrets(kind Kind, id string, sealed map[string]string) map[string]string {
	out := make(map[string]string, len(sealed))
	for k, v := range sealed {
		if s.box == nil {
			out[k] = v
			continue
		}
		plain, err := s.box.Open(v)
		if err != nil {
			slog.Warn("settingitems: secret decryption failed; blanking field",
				"kind", string(kind), "id", id, "field", k, "err", err)
			out[k] = ""
			continue
		}
		out[k] = plain
	}
	return out
}

func toAnyMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
