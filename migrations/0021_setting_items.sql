-- Reference copy of the setting_items / setting_bindings DDL. The executed
-- schema is internal/storage/schema/postgres.sql (embedded); this file exists
-- only so the migration history stays readable.
CREATE TABLE IF NOT EXISTS setting_items (
    kind        TEXT NOT NULL,
    id          TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    position    INTEGER NOT NULL DEFAULT 0,
    config      JSONB NOT NULL DEFAULT '{}',
    secrets     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, id)
);
CREATE INDEX IF NOT EXISTS setting_items_kind_pos_idx ON setting_items (kind, position, id);

CREATE TABLE IF NOT EXISTS setting_bindings (
    scope      TEXT NOT NULL,
    slot       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    item_id    TEXT NOT NULL DEFAULT '',
    params     JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, slot),
    CONSTRAINT setting_bindings_scope_ck CHECK (scope = 'global' OR scope LIKE 'user:%')
);
CREATE INDEX IF NOT EXISTS setting_bindings_item_idx ON setting_bindings (kind, item_id);
