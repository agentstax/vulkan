graceful shutdown
graceful database recovery (handles it fine now but could be done better with a retry backoff policy and better error messages)
debug field option which prints queue metrics like, how many are left
consider using database/sql from stdlib to remove pgx dependency (might be a bad idea)
current impl of a transactional enqueue (producer) doesn't support fanning out ie publishing to multiple queues
consider normalizing message log attempts into seperate append only table - so we can better track each attempt / attempted_at / error mainly for debugging / auditting. main code should not read this as it would slow things down
internal pkg logging needs to be able to pass a logger interface that is common