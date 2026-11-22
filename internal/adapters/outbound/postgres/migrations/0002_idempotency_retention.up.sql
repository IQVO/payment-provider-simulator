-- The retention index (ADR 0005, addendum 2026-11-22): the hourly purge
-- picks its bounded batch oldest-first by created_at; without the index each
-- batch is a sequential scan plus sort of the whole table.
CREATE INDEX idempotency_records_created_at ON idempotency_records (created_at);
