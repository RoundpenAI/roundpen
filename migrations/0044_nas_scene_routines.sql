-- Product-keyed seed routines for the NAS scene pack (e.g. morning OnThisDay).
-- No FK to routines: if the user deletes the routine we keep the row so Ensure
-- does not recreate it.
CREATE TABLE IF NOT EXISTS nas_scene_routines (
    user_id    TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    seed_key   TEXT NOT NULL,
    routine_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, seed_key)
);

CREATE INDEX IF NOT EXISTS nas_scene_routines_routine_idx
    ON nas_scene_routines (routine_id);
