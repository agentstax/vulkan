-- TODO should play around with these at the end when 'done' / 'dead' are seperate tables
-- This should in theory speed up claiming but it might slow down deletions (moves) and could cause more vaccuums (lots of dead tuples)
CREATE INDEX IF NOT EXISTS idx_claim_ready ON message_log (can_run_after) WHERE status = 'ready';
CREATE INDEX IF NOT EXISTS idx_claim_processing ON message_log (lease_until) WHERE status = 'processing'; -- TODO determine if want to use Id as well