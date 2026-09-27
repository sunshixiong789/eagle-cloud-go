-- +goose Up
-- +goose StatementBegin
CREATE TABLE products (
    id          bigserial     PRIMARY KEY,
    sku         varchar(64)   NOT NULL UNIQUE,
    name        varchar(128)  NOT NULL,
    description varchar(2048) NOT NULL DEFAULT '',
    price_cents bigint        NOT NULL CHECK (price_cents > 0),
    active      boolean       NOT NULL DEFAULT true,
    created_at  timestamptz   NOT NULL DEFAULT now(),
    updated_at  timestamptz   NOT NULL DEFAULT now()
);
CREATE INDEX idx_products_active_created ON products (active, created_at);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS products;
