-- +goose Up
ALTER TABLE stored_file
    ADD COLUMN state varchar(16) NOT NULL DEFAULT 'ready',
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(),
    ADD CONSTRAINT chk_stored_file_state CHECK (state IN ('pending', 'ready', 'deleting'));
ALTER TABLE stored_file ALTER COLUMN state SET DEFAULT 'pending';
CREATE INDEX idx_stored_file_state_updated ON stored_file (state, updated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_stored_file_state_updated;
ALTER TABLE stored_file
    DROP CONSTRAINT IF EXISTS chk_stored_file_state,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS state;
