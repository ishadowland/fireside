# Phase 2 — Minimal Demo Sprint (Sprint 1)

> **Status**: DCP-2 (Planning) — awaiting DCP-3 (Development) kickoff
> **Created**: 2026-08-02
> **Last reconciled with remote `main`**: 2026-08-02 (merge commit `dc6de96`)
> **Owner**: liuyin (ishadowland)
> **Sprint window**: ~2 weeks (normal estimate 21d, pessimistic 28d)
> **Target**: 2 browsers in dashboard can create rooms, join, and exchange
> messages in real time. Android lands in Sprint 1.5.

> **⚠️ Reconciliation note**: Sprint 1-1 / 1-2 / 1-3 + CI hardening +
> Issue #1 were implemented by a parallel agent on remote `main` between
> 2026-07-31 and 2026-08-02, **before this RFC was written**. As a result,
> several items in §2.3 (ULID migration, replay defense) are already
> implemented; this RFC's remaining scope (WP-1 through WP-10) is still
> open work for rooms + messages + hub + WS frames + dashboard.

---

## 1. Goal (一句话目标)

**在 dashboard 上:host 创建房间 → 拉用户进房间 → 人类发消息 → 房间内所有人实时收到。Android 同步出一个房间列表 + 房间消息流 UI(Sprint 1.5 落地)。**

---

## 2. Scope

### 2.1 In-Scope

- Rooms: create / list / get / end (host only) / participant join / leave
- Messages: send / list / system events (join / leave / ended)
- WebSocket hub: per-connection multi-room subscribe model
- REST: 6 new endpoints (rooms CRUD-lite + messages + participants)
- Dashboard UI: room list + create + single-room chat
- Android: Sprint 1.5 only (see §6)
- JWT: 1h access token + refresh token (7d)
- Auth: stub code 1234 + display_name collection

### 2.2 Out-of-Scope (explicitly NOT in Sprint 1)

- ❌ Agent of any kind (tool / custom / lobster) — Sprint 2
- ❌ Workspace / git / MD collaboration — Sprint 4
- ❌ Archive / 纪要 agent — Sprint 3
- ❌ Room announcement editing (D29) — Sprint 2
- ❌ Three-Sages / D26-D28 — Sprint 3+
- ❌ RaiseHand / 大厅审批 — Sprint 3
- ❌ ULID migration (沿用 BIGINT) — Sprint 2 (see ADR-0014)
- ❌ Replay defense (InsertToken query exists but not called) — Sprint 1.5
- ❌ Real SMS provider — Sprint 2+
- ❌ Multi-device login / concurrent sessions per user — Sprint 2+
- ❌ HTTPS / CORS production hardening — Sprint 2+
- ❌ Redis / cluster — Sprint 3+ (ADR-0013 deferred)
- ❌ Real Android UI — Sprint 1.5 (deferred from Sprint 1 per Q13)

### 2.3 Deviations from existing ADRs / design docs

> **Status as of 2026-08-02 merge with remote `main`**: items marked ✅
> have already been implemented in remote commits and **supersede** the
> Sprint 1 deviations planned here. The RFC is kept as the *forward-looking
> plan* for WP-1 through WP-10 (rooms / messages / hub / WS / dashboard),
> with the deviation notes retained so the audit trail of "why" is preserved.

| Original decision | Sprint 1 plan | Remote status (2026-08-02) |
|---|---|---|
| D3: Max 50 participants per room | Max **8** (Q7) | ⏳ Evaluate in WP-1 schema |
| D6: Ephemeral messages, room end clears | Room end **not implemented** (Q3); `keep_messages_on_end` flag (Q4) | ⏳ Evaluate in WP-2 room model |
| ADR-0014: Sprint 1 ULID migration | Defer to Sprint 2 (Q1) | ✅ **Done** in Sprint 1-3 (`0739943`) |
| ADR-0007 §Risks → Replay: `InsertToken` mandatory | Not called in Sprint 1; Sprint 1.5 (WP-9.6) | ✅ **Done** in Sprint 1-2 (`dbe0a82`) |
| design/04 state machine: Room.active → ending → ended | `ending` / `ended` not implemented | ⏳ Evaluate in WP-2 room model |
| design/01: User has `display_name` field | Add in this sprint (Q6) | ⏳ Implement in WP-1 migration `0005_users_display_name` |
| Issue #1: `TestValidateTampered` fails | Fix in WP-0.1 | ✅ **Done** (`d738757`) — by tampering payload, not signature |

---

## 3. Decisions Locked (Q1-Q25)

### A. Scope / Data

| # | Decision | Choice |
|---|---|---|
| Q1 | rooms.id type | **BIGINT** (defer ULID to Sprint 2) |
| Q2 | Room name uniqueness | **Allow duplicates** (UI dedups) |
| Q3 | Room end strategy | **NOT implemented** in Sprint 1 |
| Q4 | Message retention | `rooms.keep_messages_on_end BOOL` default `false`; host sets at create |
| Q5 | Lobby | **Basic**: all users see all active rooms |
| Q6 | User identity | Stub 1234 + **display_name modal** after dashboard login |
| Q7 | Room capacity | **Enforce 8** (configurable 1-50 at create) |

### B. WS / Hub

| # | Decision | Choice |
|---|---|---|
| Q8 | Hub structure | `map[room_id]map[*Conn]bool` (O(1) delete) |
| Q9 | System message | **Write to messages table** + broadcast |
| Q10 | JSON field naming | **snake_case** (Sprint 0 compatible) |
| Q11 | Message ack | **Not implemented** (fire-and-forget) |
| Q12 | Multi-room | **1 conn = N rooms** via `room.subscribe` / `room.unsubscribe` |

### C. Android / JWT

| # | Decision | Choice |
|---|---|---|
| Q13 | Android priority | **Sprint 1.5**, not Sprint 1 |
| Q14 | Android verification | **Emulator + real device** (in Sprint 1.5) |
| Q15 | Android data sync | Unified `GET /v1/rooms/:id/messages?since=ts` |
| Q16 | JWT TTL | **1h access** + **7d refresh token** |
| Q17 | JWT secret source | **Env var `JWT_SECRET` + fallback `JWT_SECRET_FILE`** |

### D. Testing / CI

| # | Decision | Choice |
|---|---|---|
| Q18 | Integration tests | **Local Postgres test DB** (`fireside_test`, TRUNCATE between tests) |
| Q19 | Coverage threshold | **Not enforced** (own judgement) |
| Q20 | CI trigger | **PR-only** (push to main doesn't run CI) |

### E. Process

| # | Decision | Choice |
|---|---|---|
| Q21 | ADR frequency | **Only when decision affects > 1 WP** (estimate 1-2 ADRs this sprint) |
| Q22 | Doc sync cadence | **Per WP** (not batched) |
| Q23 | Demo format | **CLI acceptance gate only** (no video/screenshot) |
| Q24 | STATUS update cadence | **Sprint-end only** |
| Q25 | Task allocation | **Main agent writes code, owner reviews PRs** |

---

## 4. WBS (Work Breakdown Structure)

```
WP-0  Preparation
├── WP-0.1  [H4]  Fix TestValidateTampered (Issue #1)
├── WP-0.2  [H2]  Update pdcp-checklist.md
└── WP-0.3  [H2]  Create this RFC file (done)

WP-1  Data layer: rooms + participants + messages
├── WP-1.1  [H4]  Lock table schemas (from design/01)
├── WP-1.2  [H2]  0002_rooms migration (.up.sql + .down.sql)
├── WP-1.3  [H2]  0003_participants migration
├── WP-1.4  [H2]  0004_messages migration
├── WP-1.5  [H2]  0005_users_display_name + 0006_auth_tokens_refresh
├── WP-1.6  [H1]  sqlc.yaml add 3 new query files
├── WP-1.7  [H4]  sqlc generate + manual fix-up
├── WP-1.8  [H2]  Test up/down roundtrip in test DB
└── WP-1.9  [H1]  EXPLAIN ANALYZE on common queries

WP-2  internal/rooms package
├── WP-2.1  [H2]  types.go (Room struct + RoomStatus)
├── WP-2.2  [H2]  store.go (CreateRoom / GetRoom / ListActive / EndRoom)
├── WP-2.3  [H4]  store_test.go (real test DB)
├── WP-2.4  [H2]  errors.go
└── WP-2.5  [H2]  mount.go (REST routes)

WP-3  internal/messages package
├── WP-3.1  [H2]  types.go (Message struct + ContentType)
├── WP-3.2  [H2]  store.go (CreateMessage / ListByRoom / ListAfter / CountByRoom)
├── WP-3.3  [H4]  store_test.go
├── WP-3.4  [H2]  errors.go
└── WP-3.5  [H2]  mount.go (REST routes)

WP-4  internal/participants package
├── WP-4.1  [H2]  types.go (Participant + StageState)
├── WP-4.2  [H2]  store.go (Join / Leave / ListOnStage / ListByRoom)
├── WP-4.3  [H4]  store_test.go (UNIQUE constraint on double-join)
├── WP-4.4  [H2]  errors.go
└── WP-4.5  [H2]  mount.go (JoinRoom / LeaveRoom REST)

WP-5  internal/hub package (CORE)
├── WP-5.1  [H4]  Lock hub interface (Register/Unregister/BroadcastToRoom)
├── WP-5.2  [H4]  Hub struct (map[room_id]map[*Conn]bool, sync.RWMutex)
├── WP-5.3  [H2]  Register(conn, room_id, user_id)
├── WP-5.4  [H2]  Unregister(conn)
├── WP-5.5  [H4]  BroadcastToRoom (skip dead conns, auto-unregister)
├── WP-5.6  [H4]  Heartbeat: ping every 30s, close after 60s no response
├── WP-5.7  [H2]  hub_test.go (concurrent broadcast, register/unregister race)
└── WP-5.8  [H2]  Wire into main.go (lifecycle + graceful shutdown)

WP-6  WS business frames (extends internal/ws)
├── WP-6.1  [H4]  Lock protocol: msg.send / msg.created / room.subscribe /
│                  room.unsubscribe / room.ended / error / system
├── WP-6.2  [H4]  frames.go: all frame structs + Validate
├── WP-6.3  [D1]  Refactor HandleConnect: post-auth-hello dispatch loop
├── WP-6.4  [H4]  msg.send handler (persist + broadcast)
├── WP-6.5  [H2]  room.subscribe handler (hub.Register)
├── WP-6.6  [H2]  room.unsubscribe handler (hub.Unregister)
├── WP-6.7  [H2]  error handler (WriteJSON, no close)
└── WP-6.8  [H4]  frames_test.go + integration_test.go

WP-7  REST endpoints
├── WP-7.1  [H2]  POST /v1/rooms (CreateRoom)
├── WP-7.2  [H2]  GET /v1/rooms (ListActive + on_stage flags)
├── WP-7.3  [H2]  GET /v1/rooms/:id + participants
├── WP-7.4  [H2]  POST /v1/rooms/:id/messages (triggers hub broadcast)
├── WP-7.5  [H2]  GET /v1/rooms/:id/messages?since=ts
├── WP-7.6  [H2]  POST /v1/rooms/:id/join
├── WP-7.7  [H2]  POST /v1/rooms/:id/leave
├── WP-7.8  [H2]  POST /v1/rooms/:id/end (host only; Sprint 1 stub: marks ended but no cleanup)
├── WP-7.9  [H2]  POST /v1/auth/refresh (new for Q16)
├── WP-7.10 [H2]  PATCH /v1/users/me (display_name)
└── WP-7.11 [H4]  contract_test.go (happy + error per endpoint)

WP-8  Dashboard UI
├── WP-8.1  [H2]  /dashboard/rooms.html (list + create button)
├── WP-8.2  [H4]  /dashboard/room.html (message stream + input + on_stage count)
├── WP-8.3  [H4]  JS: WebSocket client extension for msg.send / msg.created
├── WP-8.4  [H2]  JS: display_name modal after stub-login
├── WP-8.5  [H2]  CSS: message bubbles (mine vs theirs)
└── WP-8.6  [H2]  Compatibility with existing Sprint 0 stub-login flow

WP-9  [Sprint 1.5 — NOT Sprint 1] Android UI
├── WP-9.1  RoomListActivity
├── WP-9.2  RoomActivity
├── WP-9.3  WsClient extension
└── WP-9.4  Emulator + real-device smoke

WP-10  Documentation + CI
├── WP-10.1 [H1]  STATUS.md (Sprint 1 status)
├── WP-10.2 [H2]  docs/api/openapi.yaml (6 endpoints + 5 frames)
├── WP-10.3 [H1]  design/01-data-model.md mark rooms/participants/messages ✅
├── WP-10.4 [H1]  design/03-protocol.md mark msg.* / room.* ✅
├── WP-10.5 [H1]  ADR-0020 (hub routing strategy, if not covered by existing ADRs)
├── WP-10.6 [H1]  README.md demo references
└── WP-10.7 [H1]  CI verify (golangci-lint + go test + integration test)
```

---

## 5. Dependency Graph

```
WP-0 (prep)
   │
   └── WP-1 (data layer)
        │
        ├── WP-2 (rooms)        ─┐
        ├── WP-3 (messages)     ─┼─ (all parallel after WP-1)
        ├── WP-4 (participants) ─┘
        │
        └── WP-5 (hub)           ←─ depends on WP-2/3/4 types
             │
             └── WP-6 (WS frames) ←─ depends on WP-5 + WP-2/3/4
                  │
                  ├── WP-7 (REST) ←─ depends on WP-2/3/4
                  │
                  ├── WP-8 (Dashboard UI) ←─ depends on WP-6 + WP-7
                  │
                  └── WP-9 (Android, Sprint 1.5) ←─ depends on WP-6
                       │
                       └── WP-10 (docs + CI)
```

**Critical path**: WP-0 → WP-1 → WP-5 → WP-6 → WP-8 → WP-10 (longest)
**Fastest dashboard demo**: WP-2 + WP-7 + WP-8 (skip WS for first cut, then add WS)

---

## 6. Time Estimates

| WP | Optimistic (H/D) | Normal (H/D) | Pessimistic (H/D) |
|---|---|---|---|
| WP-0 prep | 8h | 1d | 1d |
| WP-1 data | 1.5d | 2d | 3d |
| WP-2 rooms | 1d | 1d | 1.5d |
| WP-3 messages | 1d | 1d | 1.5d |
| WP-4 participants | 1d | 1d | 1.5d |
| WP-5 hub (CORE) | **3d** | **4d** | **6d** |
| WP-6 WS frames | 2d | 3d | 4d |
| WP-7 REST | 1.5d | 2d | 2.5d |
| WP-8 Dashboard | 1.5d | 2d | 3d |
| WP-9 Android (S1.5) | 2d | 3d | 4d |
| WP-10 docs + CI | 0.5d | 1d | 1d |
| **Total (Sprint 1)** | **~13d / 2.5w** | **~17d / 3.5w** | **~23d / 5w** |
| **Sprint 1 + 1.5** | **~15d** | **~20d** | **~27d** |

---

## 7. Acceptance Gate

### 7.1 Automated (must all pass)

```bash
go test -race ./...                     # all pass
golangci-lint run ./...                 # 0 issues
sqlc vet                                # clean
make migrate.up && make migrate.down    # idempotent
```

> Note: `TestValidateTampered` may still fail (tracked in Issue #1); allowed.

### 7.2 Dashboard demo (must work end-to-end)

Open 2 browsers at `http://localhost:18080/dashboard/`, both stub-login. Then:

1. User A creates room "测试-001"
2. User B sees room in list, clicks Join
3. User A sees "B joined" event
4. User A sends "你好"
5. **Both** see A's message in real time
6. User B sends "hello" — both see it
7. User A clicks "End Room" → both see "room.ended", room disappears from list
8. User A reopens page → room is gone (marked ended, archived)

### 7.3 Android demo (Sprint 1.5 — separate gate)

2 emulators + 1 real device. Must show same scenarios as 7.2.

### 7.4 Documentation sync (must)

- `docs/api/openapi.yaml` updated with 6 new endpoints + 5 new frames
- `docs/design/01-data-model.md` marks rooms/participants/messages as ✅ Implemented
- `docs/design/03-protocol.md` marks msg.* / room.* as ✅ Implemented
- `STATUS.md` updated to "Phase 1 — Sprint 1 complete, minimal demo verified"
- `git tag v0.2-minimal-demo` applied

### 7.5 Performance baseline (record only, not blocking)

- Broadcast latency (2 users, same room): < 100ms local
- REST P95 (`POST /v1/rooms`): < 50ms
- WS concurrent connections: 100 without crash
- DB pool: peak 5, no queueing

---

## 8. Risk Register

| # | Risk | Prob | Impact | Mitigation |
|---|---|---|---|---|
| R-1 | sqlc limitations (partial index, custom types) | M | M | Verify sqlc capability in WP-1.1; fallback to pgx hand-written queries |
| R-2 | Cross-room message leak (hub.BroadcastToRoom bug) | M | **H** | Hub tests cover normal + abnormal paths; **integration test** uses 2 rooms to verify isolation |
| R-3 | Dead connection leak (client disconnected but conn not closed) | H | M | Heartbeat ping/pong (30s/10s/60s per protocol) + conn.SetReadDeadline |
| R-4 | Race condition (2 clients send simultaneously, order scrambled) | L | M | Use DB serial PK (insert time auto-increment); client accepts "server order" |
| R-5 | Android emulator smoke breaks (like Sprint 0) | M | H | Defer Android to Sprint 1.5 (Q13); Sprint 1 only requires Dashboard |
| R-6 | Dashboard / Android frame compatibility | L | M | **WP-6.1 lock protocol** before implementation; JSON field naming (Q10 = snake_case) |
| R-7 | Room name collisions in demo ("测试-001" used twice) | L | L | Accepted per Q2; UI shows host prefix |
| R-8 | JWT expires mid-room (1h TTL too short) | L | M | UI shows "re-login" prompt; refresh token (Q16) extends to 7d |
| R-9 | Port conflict (8080) | L | L | Already on 18080 (ADR-0019), unchanged |
| R-10 | Test Postgres version drift (local vs CI) | M | M | Pin `postgres:16-alpine` in CI; local via docker-compose |

---

## 9. Resource Allocation

| Role | Who | Tasks |
|---|---|---|
| Owner (decision) | liuyin | Sign off WP-1.1 schema, WP-5.1 hub interface, WP-6.1 frame protocol, ADR-0020 |
| Main coder | fireside-bot or owner | WP-1 → WP-7 → WP-8 (full stack) |
| Android coder (Sprint 1.5) | separate agent or owner | WP-9 |
| Reviewer | liuyin | PR review + acceptance gate (7.1) |
| Documenter | whoever writes the code | WP-10 + per-WP doc sync |

**Single-machine sequence**: WP-0 → WP-1 → WP-2/3/4 (parallel branches) → WP-5 → WP-6 → WP-7 → WP-8 (dashboard demo unblocked) → WP-9 (Sprint 1.5) → WP-10

---

## 10. DCP-2 Exit Checklist (pre-development)

- [x] Goal one-liner locked
- [x] In-scope / out-of-scope explicit
- [x] 25 clarifications resolved
- [x] WBS with H/D estimates per WP
- [x] Dependency graph
- [x] Acceptance gate defined
- [x] Risk register
- [x] Resource allocation
- [x] Deviations from existing ADRs documented
- [ ] **Owner sign-off** — liuyin
- [ ] Issue tracker loaded (10 issues)
- [ ] Milestone created
- [ ] STATUS.md updated to "Sprint 1 in progress"

---

## 11. References

- `docs/requirements/00-overview.md` — D1-D29 frozen decisions
- `docs/requirements/03-decision-snapshot.md` — single-doc summary
- `docs/design/01-data-model.md` — entity definitions
- `docs/design/03-protocol.md` — frame schemas
- `docs/design/04-state-machines.md` — lifecycle diagrams
- `docs/adr/0001-0019-*.md` — architectural decisions
- `docs/rfc/phase-1-mvp.md` — Sprint 0 RFC (precedent for this one)
- `STATUS.md` — current project status

---

**End of RFC.**