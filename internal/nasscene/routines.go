package nasscene

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/RoundpenAI/roundpen/internal/routine"
)

// Seed key for the photos assistant morning OnThisDay digest.
const SeedPhotosOnThisDay = "nas.photos.onthisday"

// Routines creates standing routines for scene-pack seeds.
type Routines interface {
	Create(ctx context.Context, userID string, in routine.CreateInput) (*routine.Routine, error)
}

// ensurePhotosRoutine seeds a daily morning OnThisDay routine once per user.
// If the seed row already exists (even after the user deleted the routine), skip.
func (s *Store) ensurePhotosRoutine(ctx context.Context, userID, assistantID string) error {
	if s == nil || s.DB == nil || s.Routines == nil || assistantID == "" {
		return nil
	}
	exists, err := s.seedMapped(ctx, userID, SeedPhotosOnThisDay)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	rt, err := s.Routines.Create(ctx, userID, routine.CreateInput{
		AssigneeAssistantID: assistantID,
		Title:               "往年今日",
		Brief: "每天早上扫描已授权相册目录，用 OnThisDay 找出往年今日的照片，" +
			"写一段简短中文摘要（挑几张亮点即可）。没有授权或没有匹配就说明原因后 FinishRun。" +
			"不要批量改文件，除非用户在对话里另有安排。",
		Autonomy:       routine.AutonomyRead,
		Cron:           "0 8 * * *",
		Timezone:       "Asia/Shanghai",
		MaxDurationSec: 900,
	})
	if err != nil {
		return fmt.Errorf("seed on-this-day routine: %w", err)
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO nas_scene_routines (user_id, seed_key, routine_id, created_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (user_id, seed_key) DO NOTHING`,
		userID, SeedPhotosOnThisDay, rt.ID)
	return err
}

func (s *Store) seedMapped(ctx context.Context, userID, seedKey string) (bool, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `
		SELECT routine_id FROM nas_scene_routines
		WHERE user_id=$1 AND seed_key=$2`, userID, seedKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return id != "", nil
}
