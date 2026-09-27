-- +goose Up
-- +goose StatementBegin
CREATE TABLE purchase_order (
    id            varchar(36)  PRIMARY KEY,
    owner_subject varchar(128) NOT NULL,
    status        varchar(32)  NOT NULL,
    total_cents   bigint       NOT NULL CHECK (total_cents > 0),
    created_at    timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_purchase_order_owner_created ON purchase_order (owner_subject, created_at);

CREATE TABLE order_item (
    id               bigserial    PRIMARY KEY,
    order_id         varchar(36)  NOT NULL REFERENCES purchase_order (id) ON DELETE CASCADE,
    product_id       bigint       NOT NULL,
    product_sku      varchar(64)  NOT NULL,
    product_name     varchar(128) NOT NULL,
    unit_price_cents bigint       NOT NULL CHECK (unit_price_cents > 0),
    quantity         integer      NOT NULL CHECK (quantity > 0),
    subtotal_cents   bigint       NOT NULL CHECK (subtotal_cents > 0),
    UNIQUE (order_id, product_id)
);
CREATE INDEX idx_order_item_order ON order_item (order_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS order_item;
DROP TABLE IF EXISTS purchase_order;
-- +goose StatementEnd
