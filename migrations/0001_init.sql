-- migrations/0001_init.sql
BEGIN;

CREATE TABLE IF NOT EXISTS products (
    id          BIGSERIAL PRIMARY KEY,
    sku         TEXT          NOT NULL UNIQUE,
    name        TEXT          NOT NULL,
    price       NUMERIC(12,2) NOT NULL CHECK (price > 0),
    stock       INTEGER       NOT NULL DEFAULT 0 CHECK (stock >= 0),
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS orders (
    id           BIGSERIAL PRIMARY KEY,
    status       TEXT        NOT NULL DEFAULT 'Reserved'
                             CHECK (status IN ('Reserved','Cancelled')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    cancelled_at TIMESTAMPTZ NULL
);

CREATE TABLE IF NOT EXISTS order_items (
    order_id       BIGINT NOT NULL REFERENCES orders(id)   ON DELETE CASCADE,
    product_id     BIGINT NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    quantity       INTEGER NOT NULL CHECK (quantity > 0),
    price_at_order NUMERIC(12,2) NOT NULL CHECK (price_at_order > 0),
    PRIMARY KEY (order_id, product_id)
);

CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_order_items_product ON order_items (product_id);

COMMIT;