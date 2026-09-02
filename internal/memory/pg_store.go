package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	pgvector "github.com/pgvector/pgvector-go"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// PgStore persists memory rows in PostgreSQL.
type PgStore struct {
	db *storage.DB
}

// NewPgStore returns a Store backed by PostgreSQL.
func NewPgStore(db *storage.DB) *PgStore {
	return &PgStore{db: db}
}

const shortCols = `id, session_id, payload, expires_at, created_at`

func scanShort(row interface{ Scan(dest ...any) error }) (ShortEntry, error) {
	var e ShortEntry
	var payload []byte
	var expires sql.NullTime
	var created time.Time
	err := row.Scan(&e.ID, &e.SessionID, &payload, &expires, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return ShortEntry{}, ErrNotFound
	}
	if err != nil {
		return ShortEntry{}, err
	}
	e.Payload = payload
	if expires.Valid {
		t := expires.Time.UTC()
		e.ExpiresAt = &t
	}
	e.CreatedAt = created.UTC()
	return e, nil
}

func (s *PgStore) PutShort(ctx context.Context, e ShortEntry) error {
	if e.Payload == nil {
		e.Payload = []byte("{}")
	}
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO memory_short (id, session_id, payload, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			session_id = EXCLUDED.session_id,
			payload = EXCLUDED.payload,
			expires_at = EXCLUDED.expires_at`,
		e.ID, e.SessionID, e.Payload, nullTime(e.ExpiresAt), e.CreatedAt.UTC(),
	)
	return err
}

func (s *PgStore) GetShort(ctx context.Context, id string) (ShortEntry, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT `+shortCols+` FROM memory_short WHERE id = $1`, id)
	return scanShort(row)
}

func (s *PgStore) ListShort(ctx context.Context, sessionID string) ([]ShortEntry, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT `+shortCols+` FROM memory_short
		 WHERE session_id = $1
		   AND (expires_at IS NULL OR expires_at > now())
		 ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShortEntry
	for rows.Next() {
		e, err := scanShort(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PgStore) DeleteShort(ctx context.Context, id string) error {
	res, err := s.db.SQL.ExecContext(ctx, `DELETE FROM memory_short WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PgStore) DeleteExpiredShort(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.SQL.ExecContext(ctx, `
		DELETE FROM memory_short WHERE expires_at IS NOT NULL AND expires_at < $1`, now.UTC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

const longCols = `id, agent_id, user_id, kind, content, metadata, COALESCE(source_session_id, ''),
	importance, last_used_at, use_count, expires_at, created_at, updated_at`

func scanLong(row interface{ Scan(dest ...any) error }) (LongEntry, error) {
	var e LongEntry
	var metaRaw []byte
	var imp int
	var lastUsed, expires sql.NullTime
	var created, updated time.Time
	err := row.Scan(
		&e.ID, &e.AgentID, &e.UserID, &e.Kind, &e.Content, &metaRaw, &e.SourceSessionID,
		&imp, &lastUsed, &e.UseCount, &expires, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return LongEntry{}, ErrNotFound
	}
	if err != nil {
		return LongEntry{}, err
	}
	e.Importance = imp
	e.Metadata = map[string]string{}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &e.Metadata)
	}
	if lastUsed.Valid {
		t := lastUsed.Time.UTC()
		e.LastUsedAt = &t
	}
	if expires.Valid {
		t := expires.Time.UTC()
		e.ExpiresAt = &t
	}
	e.CreatedAt = created.UTC()
	e.UpdatedAt = updated.UTC()
	return e, nil
}

func (s *PgStore) PutLong(ctx context.Context, e LongEntry, embedding []float32) error {
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	if meta == nil {
		meta = []byte("{}")
	}
	var embed any
	if len(embedding) > 0 {
		embed = pgvector.NewVector(embedding)
	}
	var src any
	if e.SourceSessionID != "" {
		src = e.SourceSessionID
	}
	if e.Kind == "" {
		e.Kind = LongFact
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	_, err = s.db.SQL.ExecContext(ctx, `
		INSERT INTO memory_long (
			id, agent_id, user_id, kind, content, metadata, source_session_id, embedding,
			importance, last_used_at, use_count, expires_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (id) DO UPDATE SET
			agent_id = EXCLUDED.agent_id,
			user_id = EXCLUDED.user_id,
			kind = EXCLUDED.kind,
			content = EXCLUDED.content,
			metadata = EXCLUDED.metadata,
			source_session_id = EXCLUDED.source_session_id,
			embedding = COALESCE(EXCLUDED.embedding, memory_long.embedding),
			importance = EXCLUDED.importance,
			last_used_at = EXCLUDED.last_used_at,
			use_count = EXCLUDED.use_count,
			expires_at = EXCLUDED.expires_at,
			updated_at = EXCLUDED.updated_at`,
		e.ID, e.AgentID, e.UserID, string(e.Kind), e.Content, meta, src, embed,
		e.Importance, nullTime(e.LastUsedAt), e.UseCount, nullTime(e.ExpiresAt),
		e.CreatedAt.UTC(), e.UpdatedAt.UTC(),
	)
	return err
}

func (s *PgStore) GetLong(ctx context.Context, id string) (LongEntry, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT `+longCols+` FROM memory_long WHERE id = $1`, id)
	return scanLong(row)
}

func appendLongFilters(q string, args []any, f LongFilter) (string, []any) {
	if f.AgentID != "" {
		args = append(args, f.AgentID)
		q += ` AND agent_id = $` + strconv.Itoa(len(args))
	}
	if f.UserID != "" {
		args = append(args, f.UserID)
		q += ` AND user_id = $` + strconv.Itoa(len(args))
	}
	if f.RunID != "" {
		args = append(args, f.RunID)
		q += ` AND source_session_id = $` + strconv.Itoa(len(args))
	}
	if f.Kind != "" {
		args = append(args, string(f.Kind))
		q += ` AND kind = $` + strconv.Itoa(len(args))
	}
	if f.MinScore > 0 {
		args = append(args, f.MinScore)
		q += ` AND importance >= $` + strconv.Itoa(len(args))
	}
	q += ` AND (expires_at IS NULL OR expires_at > now())`
	return q, args
}

func (s *PgStore) ListLong(ctx context.Context, f LongFilter) ([]LongEntry, error) {
	q := `SELECT ` + longCols + ` FROM memory_long WHERE 1=1`
	var args []any
	q, args = appendLongFilters(q, args, f)
	q += ` ORDER BY importance DESC, created_at DESC`
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit)
	q += ` LIMIT $` + strconv.Itoa(len(args))
	if f.Offset > 0 {
		args = append(args, f.Offset)
		q += ` OFFSET $` + strconv.Itoa(len(args))
	}
	rows, err := s.db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLongRows(rows)
}

func (s *PgStore) CountLong(ctx context.Context, f LongFilter) (int64, error) {
	q := `SELECT COUNT(*) FROM memory_long WHERE 1=1`
	var args []any
	q, args = appendLongFilters(q, args, f)
	var n int64
	err := s.db.SQL.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

func (s *PgStore) SearchLong(ctx context.Context, query []float32, f SearchFilter, limit int) ([]ScoredMemory, error) {
	if limit <= 0 {
		limit = 10
	}
	if len(query) == 0 {
		entries, err := s.ListLong(ctx, LongFilter{
			AgentID: f.AgentID, UserID: f.UserID, RunID: f.RunID, Kind: f.Kind, Limit: limit,
		})
		if err != nil {
			return nil, err
		}
		out := make([]ScoredMemory, len(entries))
		for i, e := range entries {
			out[i] = ScoredMemory{LongEntry: e, Score: 0}
		}
		return out, nil
	}

	qv := pgvector.NewVector(query)
	sqlQ := `SELECT ` + longCols + `, (1 - (embedding <=> $1))::float8 AS score
		FROM memory_long WHERE embedding IS NOT NULL`
	args := []any{qv}
	if f.AgentID != "" {
		args = append(args, f.AgentID)
		sqlQ += ` AND agent_id = $` + strconv.Itoa(len(args))
	}
	if f.UserID != "" {
		args = append(args, f.UserID)
		sqlQ += ` AND user_id = $` + strconv.Itoa(len(args))
	}
	if f.RunID != "" {
		args = append(args, f.RunID)
		sqlQ += ` AND source_session_id = $` + strconv.Itoa(len(args))
	}
	if f.Kind != "" {
		args = append(args, string(f.Kind))
		sqlQ += ` AND kind = $` + strconv.Itoa(len(args))
	}
	sqlQ += ` AND (expires_at IS NULL OR expires_at > now())
		ORDER BY embedding <=> $1
		LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, limit)

	rows, err := s.db.SQL.QueryContext(ctx, sqlQ, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ScoredMemory
	for rows.Next() {
		var e LongEntry
		var metaRaw []byte
		var imp int
		var lastUsed, expires sql.NullTime
		var created, updated time.Time
		var score float64
		if err := rows.Scan(
			&e.ID, &e.AgentID, &e.UserID, &e.Kind, &e.Content, &metaRaw, &e.SourceSessionID,
			&imp, &lastUsed, &e.UseCount, &expires, &created, &updated, &score,
		); err != nil {
			return nil, err
		}
		e.Importance = imp
		e.Metadata = map[string]string{}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &e.Metadata)
		}
		if lastUsed.Valid {
			t := lastUsed.Time.UTC()
			e.LastUsedAt = &t
		}
		if expires.Valid {
			t := expires.Time.UTC()
			e.ExpiresAt = &t
		}
		e.CreatedAt = created.UTC()
		e.UpdatedAt = updated.UTC()
		out = append(out, ScoredMemory{LongEntry: e, Score: score})
	}
	return out, rows.Err()
}

func scanLongRows(rows *sql.Rows) ([]LongEntry, error) {
	var out []LongEntry
	for rows.Next() {
		e, err := scanLong(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PgStore) TouchLong(ctx context.Context, ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.db.SQL.ExecContext(ctx, `
		UPDATE memory_long
		   SET last_used_at = $1, use_count = use_count + 1, updated_at = $1
		 WHERE id = ANY($2)`, at.UTC(), ids)
	return err
}

func (s *PgStore) DeleteLong(ctx context.Context, id string) error {
	res, err := s.db.SQL.ExecContext(ctx, `DELETE FROM memory_long WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PgStore) PurgeExpiredLong(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.SQL.ExecContext(ctx, `
		DELETE FROM memory_long
		 WHERE expires_at IS NOT NULL AND expires_at < $1
		   AND importance < 30
		   AND use_count = 0`, now.UTC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *PgStore) ListMissingEmbedding(ctx context.Context, limit int) ([]LongEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT `+longCols+` FROM memory_long
		 WHERE embedding IS NULL
		 ORDER BY created_at ASC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLongRows(rows)
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
