# Sprint 0 — Subcontract Handoff

Sprint 0 (Phase 1 MVP backend skeleton) is parallelized into three independent subcontracts. Each can be picked up by a different engineer/agent without coordination beyond the interfaces pinned here.

## Dispatch matrix

| ID | Title | Effort | Depends on | Parallelizable |
|---|---|---|---|---|
| **SUB-001** | `internal/auth/` — JWT + `POST /v1/auth/login` | 90–120 min | `go.mod` exists | ✅ can start immediately |
| **SUB-003** | `internal/ws/` — WS upgrader + first-frame auth | 90–150 min | SUB-001 merged | ⏸ must wait for SUB-001 |
| **SUB-ANDROID** | Android Compose `ConnectActivity` | 2–3 hr | nothing | ✅ fully parallel (contract is the WS frame spec) |

## Dispatch order

1. **First wave (parallel)**: SUB-001 + SUB-ANDROID
2. **Second wave**: SUB-003 (needs SUB-001's `auth.Validate` to exist)

## Shared contracts

The three contracts only meet at two pinned interfaces:

1. **HTTP login**: `POST /v1/auth/login` body `{"phone":"+E164","code":"1234"}` → `{"token":"...","expires_in":900}`
2. **WS handshake**: `GET /ws/v1/connect` → first client frame `{"type":"auth.hello","token":"<jwt>"}` → server replies `{"type":"auth.welcome","user_id":N,"jti":"..."}` or closes with `auth.error`.

These are the only cross-package facts the three sub-contractors must agree on. Everything else (Go package layout, Android theme, test framework choice) is locally decided.

## Sprint 0 scope notes (intentional divergence from locked design docs)

The three subcontracts ship a **subset** of `docs/design/03-protocol.md` to keep Sprint 0 a wiring exercise. These divergences are recorded as ADRs so Sprint 1 can close them:

- **`auth.hello` payload = `{type, token}` only.** `client_version` and `device_id` from the protocol doc are deferred to Sprint 1+ (Android client and server both ignore them for now; server still accepts them if present for forward compat, but does not validate). See ADR-0014 (notes block) and the existing SUB-003 protocol.go.
- **`user_id` is `int64` in Sprint 0**, not the ULID string the design doc shows. The handoff specs (`SUB-001`, `SUB-003`, `SUB-ANDROID`) all use `int64` / `Long` deliberately. See **ADR-0014** for the Sprint 1 migration plan.
- **WS close code on auth failure = 1008** (RFC 6455 "policy violation"), not the originally drafted 4001. See **ADR-0007** (amended 2026-07-27).

## Acceptance gate

When all three report done, the contract owner (Hermes / project owner) does the end-to-end verification:

```
# 1. Boot backend
make db.up
make backend.run

# 2. Get token
TOKEN=$(curl -s -X POST localhost:18080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"phone":"+861****8000","code":"1234"}' | jq -r .token)

# 3. Round-trip via wscat (SUB-003 proof)
wscat -c "ws://localhost:18080/ws/v1/connect"
> {"type":"auth.hello","token":"$TOKEN"}
< {"type":"auth.welcome","user_id":42,"jti":"...","server_time":1753...}

# 4. Android emulator (SUB-ANDROID proof) — paste $TOKEN, tap Connect, see ✅ connected.
```

All three exit successfully → Sprint 0 closed → Phase 1 starts.

## What the contract owner (me) does in parallel

While sub-contractors run, I personally do:

- `go mod init github.com/ishadowland/fireside` + Gin/WS/JWT/sqlc deps
- `make db.up` + write `db/migrations/0001_init.sql` + sqlc generate
- `.golangci.yml` (only after Go code exists to lint)
- README "Current Phase" badge update
- Final end-to-end smoke + STATUS.md update + commit/push

These cannot be delegated because they are the **integration points** the three sub-contracts land into.