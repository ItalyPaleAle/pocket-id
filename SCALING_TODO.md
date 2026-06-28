# Horizontal Scaling TODO

Plan: introduce an actor framework. Stateful entities that today rely on a
periodic cleanup job migrate to an actor that self-expires via an alarm — and
the corresponding cleanup job is deleted.

## Core blocker

- [ ] **Remove single-instance app lock** — DB `application_lock` exits any
  second replica on startup. Relax/remove it; prerequisite for all items below.
  (`service/app_lock_service.go`, `bootstrap/bootstrap.go`)

## In-memory state to share/coordinate

- [ ] **App config cache** — `atomic.Pointer` config only refreshes in the
  process that wrote it; add cross-instance invalidation or read-through.
- [ ] **Rate limiter** — per-process IP→limiter map yields N× limits across N
  replicas; move to shared/coordinated state.
- [ ] **Minor read caches** — version check, external JWKS, app-images extension
  map; per-instance, low risk, revisit only if needed.

## Background jobs (run on exactly one instance)

- [ ] **LDAP sync** — must not run concurrently across replicas.
- [ ] **SCIM sync** — must not run concurrently across replicas.
- [ ] **API key expiry job** — single run cluster-wide.
- [ ] **Analytics job** — single run to avoid duplicate pings.
- [ ] **GeoLite update job** — single run; tied to shared GeoLite storage below.
- [ ] **Orphaned temp file cleanup** — single run (filesystem backend only).

## Local-filesystem dependencies

- [ ] **Filesystem storage backend** — local disk isn't shared; require DB/S3
  backend for multi-instance.
- [ ] **Default profile picture caching** — generated avatars written to local
  disk per-instance; rely on shared storage.
- [ ] **GeoLite DB on local disk** — per-instance copy + download; use shared or
  coordinated storage.

## Migrate to actor + alarm (and delete the cleanup job)

Each of these is currently expired by `db_cleanup_job.go`. Move expiry into the
owning actor's alarm and remove the matching cleanup function.

- [ ] **WebAuthn sessions** — actor with expiry alarm; drop `ClearWebauthnSessions`.
- [ ] **One-time access tokens** — actor with expiry alarm; drop `ClearOneTimeAccessTokens`.
- [ ] **Signup tokens** — actor with expiry alarm; drop `ClearSignupTokens`.
- [ ] **Email verification tokens** — actor with expiry alarm; drop `ClearEmailVerificationTokens`.
- [ ] **OIDC authorization codes** — actor with expiry alarm; drop `ClearOidcAuthorizationCodes`.
- [ ] **OIDC refresh tokens** — actor with expiry alarm; drop `ClearOidcRefreshTokens`.
- [ ] **Reauthentication tokens** — actor with expiry alarm; drop `ClearReauthenticationTokens`.
- [ ] **Audit log retention** — actor/alarm-driven pruning; drop `ClearAuditLogs`.
- [ ] **Unused default profile pictures** — alarm-driven pruning; drop `ClearUnusedDefaultProfilePictures`.

## Already fine (no action; listed to avoid re-investigating)

- [ ] **JWT signing keys** — DB-backed and shared. Only smooth the cold-start
  race where concurrent fresh instances crash-loop on key generation.
- Login sessions (stateless JWT cookies), OIDC auth/device codes, and WebAuthn
  session storage already live in the DB.
</content>
