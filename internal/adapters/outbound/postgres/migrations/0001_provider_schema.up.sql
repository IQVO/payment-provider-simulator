CREATE TABLE provider_authorizations (
    hold_id        text        PRIMARY KEY,
    scenario       text        NOT NULL CHECK (scenario IN ('approve', 'expire-early')),
    reference      text        NOT NULL,
    currency       text        NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    held_minor     bigint      NOT NULL CHECK (held_minor >= 0),
    captured_minor bigint      NOT NULL CHECK (captured_minor >= 0),
    refunded_minor bigint      NOT NULL CHECK (refunded_minor >= 0),
    status         text        NOT NULL CHECK (status IN ('AUTHORIZED', 'CAPTURED', 'VOIDED', 'SUPERSEDED')),
    expires_at     timestamptz NOT NULL,
    created_at     timestamptz NOT NULL,
    superseded_by  text,
    movements      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    CHECK (captured_minor <= held_minor),
    CHECK (refunded_minor <= captured_minor)
);
CREATE INDEX provider_authorizations_reference_idx ON provider_authorizations (reference);

-- The first answer per (operation, Idempotency-Key). answer stays NULL only
-- inside the transaction that claimed the pair.
CREATE TABLE idempotency_records (
    operation   text        NOT NULL,
    key         text        NOT NULL,
    fingerprint text        NOT NULL,
    answer      jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (operation, key)
);
