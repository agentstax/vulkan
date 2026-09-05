# Public API

consider getting rid of admin after client looks and feels good
- would need to make client init lots of other things (which is fine)

# Doc

SEO for markdown - tags

doc decision indexing
- is our decision index doing anything? grep and python search is quite powerful
- can we make our index more like a search engine ie a list of keywords or tags -> loaded in to context on startup

# Review

Probably should have one more table name and column review (this will be hard to change later)

should probably look to see if we could speed up claim query its gotten unruly with ctes and conditionals

need to make sure we do some manual testing for cli, metrics and alerts

need another review to make sure we are not logging any sensitive information like payload

# Other

cleanup justfile

we need a long 1hr repeatable live test:
- spins up db, multi producer and multi consumers (built images)
- producers are variadic - zero load, low load, high load, ddos territory
- produces report at end of system - failed produces, skipped (dropped) messages, retries
- We can build on this overtime but keep it relatively simple at start
- Might make sense to roll this into bench as a correctness or reliability benchmark
- one of the things I want to make sure we do well here is message processing tracking ie
  do all messages get processed into delivery_log with deliveryLogMode: all on. I'm sort of
  concerned with some edge cases we could miss a message or two
