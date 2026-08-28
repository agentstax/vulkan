# Doc

add a dedicate hero section giving the low down of repo. 'Its kafka on postgres.'

Should be able to click on my user profile could make it funny or a reference
- I like the idea of profile doing stanely parable reference "The end is never the end.. 
  is never the end.. is never the end" fits well with the never ending work of this project

need to cleanup and refactor website sandbox and database code they have grown to large and unruly
make sure we do a css review after website done to make sure we can make new layers of simplfy / standize structures

Need to do a multi browser / version compatibility review

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