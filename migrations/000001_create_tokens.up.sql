CREATE TABLE IF NOT EXISTS currencies (
    id SERIAL PRIMARY KEY,
    coin VARCHAR(10) NOT NULL,
    price DOUBLE PRECISION NOT NULL,
    timestamp BIGINT NOT NULL
);

CREATE INDEX idx_currencies_coin_timestamp ON currencies (coin, timestamp);