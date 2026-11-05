-- NAS scene pack: per-user enable flag + mapping from template key → assistant.
CREATE TABLE IF NOT EXISTS nas_scene_prefs (
    user_id    TEXT PRIMARY KEY REFERENCES users (username) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS nas_scene_assistants (
    user_id       TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    template_key  TEXT NOT NULL,
    assistant_id  TEXT NOT NULL REFERENCES assistants (id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, template_key)
);

CREATE INDEX IF NOT EXISTS nas_scene_assistants_assistant_idx
    ON nas_scene_assistants (assistant_id);
