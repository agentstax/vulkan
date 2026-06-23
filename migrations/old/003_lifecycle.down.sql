-- Remove lifecycle management
ALTER TABLE IF EXISTS message_log
DROP COLUMN IF EXISTS status,
DROP COLUMN IF EXISTS attempts,
DROP COLUMN IF EXISTS can_run_after,
DROP COLUMN IF EXISTS lease_until,
DROP COLUMN IF EXISTS lease_token,
DROP COLUMN IF EXISTS last_error;