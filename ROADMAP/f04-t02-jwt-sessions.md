# JWT login and refresh sessions

## Goal
Authenticate the configured owner with password and TOTP or a one-use recovery code, then issue restart-stable access JWTs and revocable refresh sessions.

## Tasks
- [x] Build a fixed Goppy-compatible EdDSA signer from a purpose-derived key and strictly verify issuer, audience, expiry, and owner/session claims → Verified signature, restart stability, and malformed/expired rejection.
- [x] Add transactional owner login, one-use refresh rotation, logout, and direct session revocation with a seven-day absolute lifetime → Verified SQLite state transitions and replay rejection.
- [x] Update F04 roadmap/context/QA with auth contract and actual verification results → F04-T03 remains pending.
- [x] Run full tests/build/lint, inspect the task-scoped diff, and commit F04-T02.

## Done When
- [x] Wrong password or second factor yields no session; recovery codes work once; expired/revoked/replayed refresh tokens are rejected; logout stops refresh; JWT signature remains verifiable after manager reconstruction.
