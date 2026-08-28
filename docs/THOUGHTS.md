# Doc

Need an actual good table DDL generator. Ideally something that can do it live, probably some tool for it

# Other

need to exaimine a table DDL diagram before doing final public surface / documentation
related should audit / evaluate all table columns for consistency in naming

need AGPLv3 liscense

we need a long 1hr repeatable live test:
- spins up db, multi producer and multi consumers (built images)
- producers are variadic - zero load, low load, high load, ddos territory
- produces report at end of system - failed produces, skipped (dropped) messages, retries
- We can build on this overtime but keep it relatively simple at start

need another review to make sure we are not logging any sensitive information like payload