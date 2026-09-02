package llmgw

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Store persists llmgw vault rows and transaction logs in PostgreSQL.
type Store struct {
	db *storage.DB
}

// NewStore wraps a Roundpen DB.
func NewStore(db *storage.DB) *Store {
	return &Store{db: db}
}

// UpsertUpstream inserts or updates a provider vault row.
func (s *Store) UpsertUpstream(u Upstream) error {
	modelMap, err := json.Marshal(u.ModelMap)
	if err != nil {
		return err
	}
	if modelMap == nil {
		modelMap = []byte("{}")
	}
	patterns, err := json.Marshal(u.ModelPatterns)
	if err != nil {
		return err
	}
	if patterns == nil {
		patterns = []byte("[]")
	}
	_, err = s.db.SQL.Exec(`
		INSERT INTO llmgw_upstreams (provider, base_url, api_key, model_map, model_patterns, enabled, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (provider) DO UPDATE SET
			base_url=EXCLUDED.base_url,
			api_key=EXCLUDED.api_key,
			model_map=EXCLUDED.model_map,
			model_patterns=EXCLUDED.model_patterns,
			enabled=EXCLUDED.enabled,
			updated_at=EXCLUDED.updated_at`,
		u.Provider, u.BaseURL, u.APIKey, modelMap, patterns, u.Enabled, u.UpdatedAt.UTC(),
	)
	return err
}

// GetUpstream returns an enabled upstream by provider.
func (s *Store) GetUpstream(ctx context.Context, provider string) (*Upstream, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT provider, base_url, api_key, model_map, model_patterns, enabled, updated_at
		FROM llmgw_upstreams WHERE provider=$1 AND enabled=true`, provider)
	return scanUpstream(row)
}

// ListUpstreams returns all upstream rows (including disabled).
func (s *Store) ListUpstreams(ctx context.Context) ([]Upstream, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT provider, base_url, api_key, model_map, model_patterns, enabled, updated_at
		FROM llmgw_upstreams ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upstream
	for rows.Next() {
		u, err := scanUpstream(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func scanUpstream(row interface{ Scan(dest ...any) error }) (*Upstream, error) {
	var (
		u         Upstream
		modelMap  []byte
		patterns  []byte
		updatedAt time.Time
	)
	err := row.Scan(&u.Provider, &u.BaseURL, &u.APIKey, &modelMap, &patterns, &u.Enabled, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.UpdatedAt = updatedAt.UTC()
	u.ModelMap = map[string]string{}
	if len(modelMap) > 0 {
		_ = json.Unmarshal(modelMap, &u.ModelMap)
	}
	if len(patterns) > 0 {
		_ = json.Unmarshal(patterns, &u.ModelPatterns)
	}
	return &u, nil
}

// UpsertVirtualKey inserts or updates a virtual key.
func (s *Store) UpsertVirtualKey(vk VirtualKey) error {
	_, err := s.db.SQL.Exec(`
		INSERT INTO llmgw_virtual_keys (key, name, enabled, created_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (key) DO UPDATE SET
			name=EXCLUDED.name,
			enabled=EXCLUDED.enabled`,
		vk.Key, vk.Name, vk.Enabled, vk.CreatedAt.UTC(),
	)
	return err
}

// GetVirtualKey returns an enabled virtual key.
func (s *Store) GetVirtualKey(ctx context.Context, key string) (*VirtualKey, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT key, name, enabled, created_at
		FROM llmgw_virtual_keys WHERE key=$1 AND enabled=true`, key)
	var vk VirtualKey
	var created time.Time
	err := row.Scan(&vk.Key, &vk.Name, &vk.Enabled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	vk.CreatedAt = created.UTC()
	return &vk, nil
}

// ListVirtualKeys returns all virtual keys.
func (s *Store) ListVirtualKeys(ctx context.Context) ([]VirtualKey, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT key, name, enabled, created_at
		FROM llmgw_virtual_keys ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VirtualKey
	for rows.Next() {
		var vk VirtualKey
		var created time.Time
		if err := rows.Scan(&vk.Key, &vk.Name, &vk.Enabled, &created); err != nil {
			return nil, err
		}
		vk.CreatedAt = created.UTC()
		out = append(out, vk)
	}
	return out, rows.Err()
}

// InsertTransaction writes a relay log row (bodies optional).
func (s *Store) InsertTransaction(ctx context.Context, tx Transaction, bodies *TransactionBodies) error {
	var (
		reqBody, respBody, upBody    sql.NullString
		reqTrunc, respTrunc, upTrunc bool
	)
	if bodies != nil {
		reqBody = sql.NullString{String: bodies.Request, Valid: true}
		respBody = sql.NullString{String: bodies.Response, Valid: true}
		upBody = sql.NullString{String: bodies.UpstreamRequest, Valid: true}
		reqTrunc = bodies.RequestTruncated
		respTrunc = bodies.ResponseTruncated
		upTrunc = bodies.UpstreamRequestTruncated
	}
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO llmgw_transactions (
			request_id, virtual_key, virtual_name, provider, method, path, upstream_url,
			status_code, request_bytes, response_bytes, duration_ms, error,
			request_body, response_body, upstream_request_body,
			request_truncated, response_truncated, upstream_request_truncated, created_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,
			$8,$9,$10,$11,$12,
			$13,$14,$15,
			$16,$17,$18,$19
		)`,
		tx.RequestID, tx.VirtualKey, tx.VirtualName, tx.Provider, tx.Method, tx.Path, tx.UpstreamURL,
		tx.StatusCode, tx.RequestBytes, tx.ResponseBytes, tx.DurationMS, tx.Error,
		reqBody, respBody, upBody,
		reqTrunc, respTrunc, upTrunc, tx.CreatedAt.UTC(),
	)
	return err
}

// ListTransactions returns newest-first filtered logs.
func (s *Store) ListTransactions(ctx context.Context, opts ListOptions) ([]Transaction, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 1000 {
		opts.Limit = 1000
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, request_id, virtual_key, virtual_name, provider, method, path, upstream_url,
			status_code, request_bytes, response_bytes, duration_ms, error, created_at
		FROM llmgw_transactions
		WHERE ($1 = '' OR virtual_key = $1)
		  AND ($2 = '' OR provider = $2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4`,
		opts.VirtualKey, opts.Provider, opts.Limit, opts.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Transaction
	for rows.Next() {
		var tx Transaction
		var created time.Time
		if err := rows.Scan(
			&tx.ID, &tx.RequestID, &tx.VirtualKey, &tx.VirtualName, &tx.Provider,
			&tx.Method, &tx.Path, &tx.UpstreamURL, &tx.StatusCode,
			&tx.RequestBytes, &tx.ResponseBytes, &tx.DurationMS, &tx.Error, &created,
		); err != nil {
			return nil, err
		}
		tx.CreatedAt = created.UTC()
		out = append(out, tx)
	}
	return out, rows.Err()
}

// GetTransaction returns one log row with bodies.
func (s *Store) GetTransaction(ctx context.Context, id int64) (Transaction, *TransactionBodies, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, request_id, virtual_key, virtual_name, provider, method, path, upstream_url,
			status_code, request_bytes, response_bytes, duration_ms, error, created_at,
			request_body, response_body, upstream_request_body,
			request_truncated, response_truncated, upstream_request_truncated
		FROM llmgw_transactions WHERE id=$1`, id)

	var (
		tx                           Transaction
		created                      time.Time
		reqBody, respBody, upBody    sql.NullString
		reqTrunc, respTrunc, upTrunc bool
	)
	err := row.Scan(
		&tx.ID, &tx.RequestID, &tx.VirtualKey, &tx.VirtualName, &tx.Provider,
		&tx.Method, &tx.Path, &tx.UpstreamURL, &tx.StatusCode,
		&tx.RequestBytes, &tx.ResponseBytes, &tx.DurationMS, &tx.Error, &created,
		&reqBody, &respBody, &upBody, &reqTrunc, &respTrunc, &upTrunc,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Transaction{}, nil, storage.ErrNotFound
	}
	if err != nil {
		return Transaction{}, nil, err
	}
	tx.CreatedAt = created.UTC()

	var bodies *TransactionBodies
	if reqBody.Valid || respBody.Valid || upBody.Valid {
		bodies = &TransactionBodies{
			Request:                  reqBody.String,
			Response:                 respBody.String,
			UpstreamRequest:          upBody.String,
			RequestTruncated:         reqTrunc,
			ResponseTruncated:        respTrunc,
			UpstreamRequestTruncated: upTrunc,
		}
	}
	return tx, bodies, nil
}

// Stats aggregates traffic, optionally filtered by virtual key.
func (s *Store) Stats(ctx context.Context, virtualKey string) (Stats, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT
			COUNT(*)::bigint,
			COALESCE(SUM(request_bytes), 0)::bigint,
			COALESCE(SUM(response_bytes), 0)::bigint,
			COALESCE(AVG(duration_ms), 0)::float8,
			COUNT(*) FILTER (WHERE status_code >= 400 OR error <> '')::bigint
		FROM llmgw_transactions
		WHERE ($1 = '' OR virtual_key = $1)`, virtualKey)

	var st Stats
	if err := row.Scan(
		&st.TotalRequests, &st.TotalRequestBytes, &st.TotalResponseBytes,
		&st.AvgDurationMS, &st.ErrorCount,
	); err != nil {
		return Stats{}, err
	}
	return st, nil
}
