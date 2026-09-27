-- +goose Up
ALTER TABLE purchase_order ADD COLUMN idempotency_key varchar(64);
UPDATE purchase_order SET idempotency_key = id WHERE idempotency_key IS NULL;
ALTER TABLE purchase_order ALTER COLUMN idempotency_key SET NOT NULL;
CREATE UNIQUE INDEX uq_purchase_order_owner_idempotency
    ON purchase_order (owner_subject, idempotency_key);

ALTER TABLE event_outbox DROP CONSTRAINT IF EXISTS event_outbox_aggregate_id_event_type_key;

-- +goose Down
ALTER TABLE event_outbox
    ADD CONSTRAINT event_outbox_aggregate_id_event_type_key UNIQUE (aggregate_id, event_type);
DROP INDEX IF EXISTS uq_purchase_order_owner_idempotency;
ALTER TABLE purchase_order DROP COLUMN IF EXISTS idempotency_key;
