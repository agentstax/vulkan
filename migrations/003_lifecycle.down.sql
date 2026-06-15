-- Remove lifecycle management
ALTER TABLE IF EXISTS message_log
DROP COLUMN IF EXISTS status,
DROP COLUMN IF EXISTS attempts,
DROP COLUMN IF EXISTS can_run_after,
DROP COLUMN IF EXISTS locked_at,
DROP COLUMN IF EXISTS last_error;