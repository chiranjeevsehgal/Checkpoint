-- +goose Up
CREATE TABLE devices (
    device_id CHAR(32) PRIMARY KEY,

    user_id UUID UNIQUE,

    state TEXT NOT NULL,
    claimed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT devices_device_id_check CHECK (device_id ~ '^[0-9a-f]{32}$'),
    CONSTRAINT devices_state_check CHECK (state IN ('unowned', 'owned', 'reset_required')),
    CONSTRAINT devices_owner_check CHECK (
        (state = 'owned' AND user_id IS NOT NULL)
        OR (state <> 'owned' AND user_id IS NULL)
    )
);

CREATE TABLE device_claim_credentials (
    device_id CHAR(32) PRIMARY KEY REFERENCES devices(device_id) ON DELETE CASCADE,

    claim_hash BYTEA NOT NULL,

    CONSTRAINT device_claim_hash_len_check CHECK (octet_length(claim_hash) = 32)
);

ALTER TABLE uploads
    ADD COLUMN device_id CHAR(32) NOT NULL REFERENCES devices(device_id),
    ADD CONSTRAINT uploads_device_id_check CHECK (device_id ~ '^[0-9a-f]{32}$');

ALTER TABLE idempotency_keys
    ADD COLUMN device_id CHAR(32) NOT NULL,
    ADD CONSTRAINT idempotency_keys_device_id_check CHECK (device_id ~ '^[0-9a-f]{32}$');

ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;
ALTER TABLE idempotency_keys ADD PRIMARY KEY (user_id, device_id, key);

-- +goose Down
ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;
ALTER TABLE idempotency_keys ADD PRIMARY KEY (user_id, key);
ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_device_id_check;
ALTER TABLE idempotency_keys DROP COLUMN device_id;
ALTER TABLE uploads DROP CONSTRAINT uploads_device_id_check;
ALTER TABLE uploads DROP COLUMN device_id;
DROP TABLE IF EXISTS device_claim_credentials;
DROP TABLE IF EXISTS devices;
