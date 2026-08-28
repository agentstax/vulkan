# Doc

Doc site versioning (big one)

mobile friendly docsite

need to cleanup and refactor website sandbox and database code they have grown to large and unruly
make sure we do a css review after website done to make sure we can make new layers of simplfy / standize structures

randomized cookie popup on doc site that mocks cookies being used for a doc site
- could have cookie popup with standard yes or no and whatever user clicks be like 
  'oh sorry bro you've been hacked, we ain't got no cookies here'
  'god please no I'll do anything [links to github repo to give star]' 'accept fate'
Should be able to click on my user profile could make it funny
something funny with 'All times are UTC' as a link to something snarky about local time errors could reference some massive problem that was related to time zone error

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