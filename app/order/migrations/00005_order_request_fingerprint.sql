-- +goose Up
-- Empty values preserve compatibility with older writers during rolling deployment.
-- Existing immutable item snapshots are used to recover their canonical fingerprint.
ALTER TABLE purchase_order ADD COLUMN request_fingerprint varchar(64) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE purchase_order DROP COLUMN request_fingerprint;
