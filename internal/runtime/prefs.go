package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PrefStore persists per-user agent engine choice.
type PrefStore struct {
	DB *sql.DB
}

// AgentEngine returns the stored preference, or empty if unset.
func (s *PrefStore) AgentEngine(ctx context.Context, userID string) (string, error) {
	if s == nil || s.DB == nil {
		return "", nil
	}
	var engine string
	err := s.DB.QueryRowContext(ctx, `SELECT agent_engine FROM user_runtime WHERE user_id=$1`, userID).Scan(&engine)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return NormalizeEngine(engine), nil
}

// SetAgentEngine records qemu|docker|kern for the user.
func (s *PrefStore) SetAgentEngine(ctx context.Context, userID, engine string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("runtime preferences not configured")
	}
	engine = NormalizeEngine(engine)
	if engine == "" {
		return fmt.Errorf("engine must be qemu, docker, or kern")
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_runtime (user_id, agent_engine, updated_at)
		VALUES ($1,$2,now())
		ON CONFLICT (user_id) DO UPDATE SET
			agent_engine=EXCLUDED.agent_engine,
			updated_at=now()`, userID, engine)
	return err
}

// ResolveAgentEngine prefers the user's stored choice, else the process default.
func (s *PrefStore) ResolveAgentEngine(ctx context.Context, userID, fallback string) (string, error) {
	got, err := s.AgentEngine(ctx, userID)
	if err != nil {
		return "", err
	}
	if got != "" {
		return got, nil
	}
	if e := NormalizeEngine(fallback); e != "" {
		return e, nil
	}
	return EngineQEMU, nil
}
