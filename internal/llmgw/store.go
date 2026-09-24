package llmgw

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func hashVirtualKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func storedVirtualKey(key string) string {
	if key == "" || looksHashedKey(key) {
		return key
	}
	return hashVirtualKey(key)
}

func looksHashedKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	for _, c := range key {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func maskVirtualKey(key string) string {
	if key == "" {
		return ""
	}
	if !strings.HasPrefix(key, "vk-") || len(key) < 8 {
		return "vk-****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// Store persists llmgw vault rows and transaction logs in PostgreSQL.
// Upstream API keys are sealed at the storage boundary (AES-GCM, "enc:v1:"
// prefix); legacy plaintext rows stay readable and are re-encrypted on the
// next upsert. A nil box disables encryption (tests, single-user dev).
type Store struct {
	db  *storage.DB
	box *secretbox.Box
}

// NewStore wraps a Roundpen DB.
func NewStore(db *storage.DB, box *secretbox.Box) *Store {
	return &Store{db: db, box: box}
}

// UpsertUpstream inserts or updates a provider vault row.
func (s *Store) UpsertVirtualKey(vk VirtualKey) error {
	_, err := s.db.SQL.Exec(`
		INSERT INTO llmgw_virtual_keys (key, name, enabled, created_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (key) DO UPDATE SET
			name=EXCLUDED.name,
			enabled=EXCLUDED.enabled`,
		storedVirtualKey(vk.Key), vk.Name, vk.Enabled, vk.CreatedAt.UTC(),
	)
	return err
}

// SeedVirtualKey inserts a virtual key unless its (hashed) key row already
// exists; unlike UpsertVirtualKey it never re-enables a key an admin disabled.
func (s *Store) SeedVirtualKey(vk VirtualKey) error {
	_, err := s.db.SQL.Exec(`
		INSERT INTO llmgw_virtual_keys (key, name, enabled, created_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (key) DO NOTHING`,
		storedVirtualKey(vk.Key), vk.Name, vk.Enabled, vk.CreatedAt.UTC(),
	)
	return err
}

// GetVirtualKey returns an enabled virtual key.
func (s *Store) GetVirtualKey(ctx context.Context, key string) (*VirtualKey, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT key, name, enabled, created_at
		FROM llmgw_virtual_keys WHERE (key=$1 OR key=$2) AND enabled=true`, key, hashVirtualKey(key))
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

// DeleteVirtualKey removes a virtual key row, matching the plaintext or its
// stored hash form.
func (s *Store) DeleteVirtualKey(ctx context.Context, key string) error {
	_, err := s.db.SQL.ExecContext(ctx,
		`DELETE FROM llmgw_virtual_keys WHERE key=$1 OR key=$2`, key, hashVirtualKey(key))
	return err
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
