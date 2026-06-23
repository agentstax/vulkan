-- Add lifecycle management
ALTER TABLE IF EXISTS message_log
ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'ready',
ADD COLUMN IF NOT EXISTS attempts int NOT NULL default 0,
ADD COLUMN IF NOT EXISTS can_run_after timestamptz NOT NULL default now(),
ADD COLUMN IF NOT EXISTS lease_until timestamptz,
ADD COLUMN IF NOT EXISTS lease_token UUID,
ADD COLUMN IF NOT EXISTS last_error text;