# Bootstrap local owner with TOTP

## Goal
Add one-time local owner setup with password, confirmed TOTP, one-time recovery codes, and a local reset command.

## Tasks
- [x] Add owner auth primitives for password hashing, encrypted TOTP enrollment, recovery codes, and transactional bootstrap/reset → Verified with SQLite unit tests.
- [x] Add admin CLI commands with hidden password input and restricted master-key file loading → Command registration compiles; lint is clean for new code.
- [x] Update F04 task/context/QA records with implemented behavior and evidence → Roadmap status and validation limits recorded.
- [x] Run focused tests, repository checks, inspect the diff, and commit only F04-T01 changes.

## Done When
- [x] A second bootstrap cannot replace the owner; unconfirmed TOTP is never persisted; recovery codes are shown once and consumed once; reset clears owner auth state.
