-- Optional GitHub PR URL linked from an issue (Roundpen does not host PR threads).
ALTER TABLE issues
    ADD COLUMN IF NOT EXISTS pr_url TEXT NOT NULL DEFAULT '';
