# Status

> **Sprint 1 + Sprint 2 backlog complete.** The Phase 2 minimal demo
> is end-to-end working: backend (rooms + messages + participants +
> hub + WS business frames + refresh tokens + display_name), the
> loopback dashboard (lobby + chat), and the integration test
> infra that lets CI run the three suites in parallel.
>
> RFC: [`docs/rfc/phase-2-minimal-demo.md`](rfc/phase-2-minimal-demo.md)
> Milestone: [`Sprint 1: Minimal Demo`](https://github.com/ishadowland/fireside/milestone/1) (issues #2–#12)
> Sprint 1.5 (deferred Android): tracked separately under WP-9 (issue #11).
> Tag: `v0.2-minimal-demo` exists in the history; a `v0.3-wp7-wp8` tag
> is the next natural cut after the reviewer signs off.

Last updated: 2026-08-05

## Where we are

**Sprint 1 is functionally complete and Sprint 2's WP-7 (refresh +
display_name) has landed.** The Sprint 0 hello-world is now
augmented with:

- the rooms / messages / participants REST surface
- a real PostgreSQL schema (CHAR(26) IDs converted to VARCHAR(26)
  in migration 0007, plus the new `refresh_tokens` table from 0008)
- an in-process broadcast hub (WP-5)
- the WS business-frame dispatch loop (WP-6) wired to the hub
- the loopback dashboard (WP-8) — lobby + chat
- refresh token rotation with replay defense (WP-7.9)
- `PATCH /v1/users/me` + `GET /v1/users/me` (WP-7.10)
- per-package test isolation so the three integration suites run
  in parallel under `go test ./...` (issue #16 L-2)
- CI Action bumps to native Node 24 (issue #16 L-3)

REST `end` and `POST messages` now fan out `room.ended` /
`msg.created` frames to WS subscribers (issue #18). Refresh token
replay triggers a family-wide revoke (issue #9 follow-up).

```text
$ curl -X POST .../v1/auth/login {phone,code:1234}
  → 200 {token, refresh_token, expires_in:900}

$ curl -X POST .../v1/auth/refresh -H "..." {refresh_token}
  → 200 {token, refresh_token, expires_in:900}    (new pair; old is rotated)

$ curl -X POST .../v1/refresh {refresh_token}   # replay
  → 401 {error: refresh_token_replayed}            (family revoked)

$ curl -X POST .../v1/rooms -H "Bearer $TOKEN" {name,max}
  → 200 {room: {id, status: "active", ...}}

$ curl -X POST .../v1/rooms/:id/join -H "Bearer $TOKEN"
  → 200 {participant: {stage_state: "on_stage", ...}}
  (also writes a system message: participant.joined)

$ curl -X POST .../v1/rooms/:id/messages -H "Bearer $TOKEN" {content}
  → 200 {message: {id, sender_kind: "human", mentions: [], ...}}

$ curl -X GET .../v1/users/me -H "Bearer $TOKEN"
  → 200 {id, phone, display_name}

$ curl -X PATCH .../v1/users/me -H "Bearer $TOKEN" -d '{"display_name":"Alice"}'
  → 200 {display_name: "Alice"}
```

The hub (`internal/hub`) is wired in `main.go`, unit-tested
(10/10 tests including cross-room isolation, dead-conn cleanup,
and 5-conn × 50-broadcast concurrent), and driven by the WP-6 WS
dispatch loop and the REST end/messages handlers (issue #18).

## What's done

### Sprint 1 REST surface (WP-1 through WP-4, 2026-08-02)

- ✅ **WP-1: data layer** — 4 migrations (rooms / participants /
  messages / users_display_name) + sqlc query files
  (hand-generated; sqlc v1.31 requires Go 1.26, not 1.22). Store
  models include `Room` / `Participant` / `Message` /
  `RefreshToken`. (commit `12550b5`, plus reviewer enum-cast fix
  `ffdbea4`)
- ✅ **WP-2: internal/rooms** — `Service` with
  CreateRoom / GetRoom / ListActive / EndRoom + 4 REST endpoints
  (POST / GET / GET :id / POST :id/end) + JWT middleware.
  (commit `9b4fb47`; reviewer fix `061690e`:
  migration 0007 CHAR(26) → VARCHAR(26) + removed 6 Trim workaround
  sites)
- ✅ **WP-3: internal/messages** — `Service` with
  CreateMessage / CreateSystemMessage / ListMessagesByRoom /
  GetMessage + 2 REST endpoints (POST / GET :id/messages) +
  cursor pagination. (commit `d38cb0e`; reviewer fix `1d80955`:
  `::stage_state` casts + `ListMessagesByRoom` 404 on unknown
  room; follow-up `448d68c`: aliased `ErrRoomNotFound` to
  `rooms.ErrRoomNotFound` to fix cross-package `errors.Is` bug)
- ✅ **WP-4: internal/participants** — `Service` with
  JoinRoom / LeaveRoom / ListOnStageByRoom / ListOnStageByUser /
  GetOnStageParticipant + 2 REST endpoints (POST join / leave).
  Capacity 8 enforced with a `SELECT ... FOR UPDATE` serializing
  capacity check (issue #19 fix); system message written on every
  transition. (commit `ccaaff5`; reviewer fix `4c89d73`: atomic
  JoinRoom + RETURNING LeaveRoom; follow-up `acf9415`: SQLSTATE 42P08
  cast fix)

### Sprint 1 hub (WP-5, 2026-08-03)

- ✅ **WP-5: internal/hub** — in-process WS broadcast hub with
  11 methods (Register / Unregister / UnregisterFromRoom /
  BroadcastToRoom / IsSubscribed / Count / RoomCount / RoomMembers /
  ConnID / StartHeartbeat / MarshalFrame). Concurrency model:
  single sync.RWMutex, atomic two-phase broadcast (snapshot under
  RLock, writes outside). Dead-conn auto-unregister on write failure.
  Wired in `main.go` and driven by WP-6 + REST end/messages handlers.
  (commit `761094f`; reviewer fix `fe1d21f`: writeMu leak fix +
  dead-conn path retest)

### Sprint 1 hub / WS frames (WP-6, 2026-08-03)

- ✅ **WP-6: internal/ws business frames** — POST-auth frame
  dispatch loop handles 4 client frames
  (`room.subscribe` / `room.unsubscribe` / `msg.send` / `heartbeat`)
  and 4 server frames (`room.subscribed` / `room.unsubscribed` /
  `msg.created` / `error`). `msg.created` is broadcast via the
  hub; `room.ended` is broadcast by the REST `end` handler via
  the hub (issue #18). Per-conn gorilla/websocket write mutex
  (`sync.Mutex` keyed by `*websocket.Conn`) prevents the
  "concurrent write" panics that would otherwise arise between
  the dispatch loop and the hub on the same conn.
  (commit `c846c9c`)

### Sprint 1 code-review fixes (4 all closed)

- ✅ **#14** (WP-3 review): hand-fixes in commits `1d80955` +
  `448d68c` + `e9a509c`
- ✅ **#15** (WP-4 review): hand-fixes in commits `4c89d73` +
  `acf9415` + `e9a509c`
- ✅ **#13** (WP-2 review): hand-fixes in commit `061690e`
- ✅ **#17** (WP-5/WP-6 review): hand-fixes in commit `fe1d21f`

### Sprint 1 Wave 1 P0 fixes (2026-08-03)

- ✅ **#18** REST `end` and `POST messages` now broadcast via the
  hub to all WS subscribers
- ✅ **#19** JoinRoom now uses a transaction with `SELECT ... FOR
  UPDATE` to serialize the capacity check + insert (concurrent
  over-capacity joiners are now correctly rejected)
- ✅ **#21** `auth.resolveUserID` is now idempotent under
  simultaneous first-login: a `SQLSTATE 23505` unique-violation
  on insert is caught (`internal/store.IsUniqueViolation`) and
  the lookup is retried
- ✅ **#22** `messages.CreateMessage` and `JoinRoom` now return
  `ErrRoomEnded` (not `ErrRoomNotFound`) for ended rooms; the REST
  mounts map to 409

(commit `e9a509c`)

### Sprint 1 Wave 2 P1 housekeeping (reviewer, 2026-08-03)

- ✅ **#20** dashboard `TestMain` now `os.Exit(m.Run())` (was
  silently green)
- ✅ **#23** drop `::CHAR(26)` casts in store files (matches
  migration 0007)
- ✅ **#24** fix `msg.send` gate comment to match code
- ✅ **#25** replay defense now verifies persisted jti's user_id
  matches the token uid claim
- ✅ **#26** JoinRoom ended-room 409 (in addition to messages
  package); docs/dead-code cleanup

(commit `1587275`)

### Sprint 1 dashboard UI (WP-8, 2026-08-04)

- ✅ **WP-8: internal/dashboard** — loopback-only dashboard
  pages: `index.html` (Sprint 0 hello-world), `rooms.html`
  (lobby + create + join), `room.html` (chat with participants
  list + message history + input + end-room). Shared `lib.js`
  module exposes `Fireside` helpers: `jwtFetch`, `login`,
  `openWS`, `escapeHtml`, `ready`. `rooms.js` and `room.js`
  implement the page-specific flows. CI guard and the existing
  `api/v1/dashboard/config` endpoint are unchanged.
  (commit `292e48a`)

### Sprint 1 / Sprint 2 backlog (WP-7 + #16 L-2/L-3, 2026-08-04)

- ✅ **WP-7.9** `POST /v1/auth/refresh` — refresh token rotation
  with replay defense. New `refresh_tokens` table (migration
  0008). 7-day TTL per RFC Q16. Old refresh token is marked
  `replaced_by_jti`; using it again triggers a family-wide revoke
  (returns 401 `refresh_token_replayed`). Login now returns
  `refresh_token` alongside the access token.
- ✅ **WP-7.10** `GET /v1/users/me` + `PATCH /v1/users/me` — new
  `internal/users` package. Read returns the current user;
  PATCH sets `display_name` (capped at 64 chars, trimmed).
- ✅ **#16 L-2** per-package test infra. New `internal/testutil`
  package gives each integration suite its own database
  (`fireside_test_rooms` / `..._messages` / `..._participants`).
  Provisioning is automatic: testutil drops the package DB,
  creates it fresh, and runs `pg_dump ... | psql` through the
  existing `fireside-postgres` Docker container to mirror the
  schema. CI workflow drops `-p 1`; the three integration suites
  now run in parallel under default `go test ./...` parallelism
  (~17s wall time vs ~28s before).
- ✅ **#16 L-3** bumped CI Actions to native Node 24:
  actions/checkout@v4 → v6,
  actions/setup-go@v5 → v7,
  golangci/golangci-lint-action@v7 → v9.

(commit `72f7676`)

### Sprint 1.5 review acceptance (2026-08-05, commit `8040ead`)

The reviewer's three commits (CI golangci-lint bump + WP-7/WP-8
batch + Sprint 1.5 refresh/testutil batch) were applied locally. The
ten issues they addressed (all now closed) are:

- **#27** testutil hard-codes container name → `discoverContainer`
  by published port
- **#28** testutil 4 golangci-lint issues → `defer` errcheck +
  explicit `_ = err` + De Morgan + drop unused `stripSlash`
- **#29** room.js hits nonexistent `/participants` → read
  `GET /v1/rooms/:id` (which carries `participants` in the same
  payload)
- **#30** dashboard timestamps blank → `fmtTime` accepts RFC3339
  strings as well as `{seconds}`
- **#31** display_name counts bytes → counts runes
  (`utf8.RuneCountInString`) so Chinese names up to 64 chars
  work
- **#32** duplicate `<meta charset>` line → drop
- **#33** refresh issues access token without persisting jti
  (bypasses ADR-0007) → persist jti after successful rotation
- **#34** replay + family-revoke path has no unit test →
  `TestRefreshHandler_ReplayRevokesFamily` + extended
  `TestRefreshHandler_RotatesToken`
- **#35** testutil password leaks into docker argv → use
  `PGPASSWORD` env + `url.Parse`-based DSN builders
- **#36** testutil without docker is unrunnable → fall back to
  the base `FIRESIDE_TEST_DSN` (parallel runs need `-p 1`)

(commit `8040ead`)

### Issue closeouts (2026-08-02 → 2026-08-05)

26 issues closed across Sprint 1 + Sprint 1.5 review (Sprint 1
only leaves #11 WP-9:

```
#1, #2, #3, #4, #5, #6, #7, #8, #9, #10, #12, #13, #14, #15, #16, #17, #18, #19, #20, #21, #22, #23, #24, #25, #26, #27, #28, #29, #30, #31, #32, #33, #34, #35, #36
```

## Sprint 1 verification (latest local run, 2026-08-04)

```
go test -race -count=1 ./...
?  cmd/fireside           [no test files]
ok internal/auth           2.7s
?  internal/config         [no test files]
ok internal/dashboard      2.2s
ok internal/hub            7.3s
ok internal/messages      17.1s
ok internal/participants  19.2s
ok internal/rooms         13.7s
?  internal/store          [no test files]
?  internal/testutil       [no test files]
?  internal/users          [no test files]
ok internal/ws             5.8s
```

20.4s wall time, race-clean, 7/7 packages green. (The slight
slowdown vs. the 17.2s reading is the reviewer testutil fallback
path exercising the docker pg_dump + psql pipeline, which is
the cost of per-package DB isolation.)

End-to-end smoke (`/tmp/wpe2e/e2e_dashboard.js`) exercises RFC
§7.2 steps 1–8 with two browser stubs, plus the new issue #22
(`POST /v1/rooms/:id/messages` after end → 409), plus the issue
#18 broadcast flow (alice + bob both receive `msg.created`).
24/24 checks pass.

WP-7 e2e (curl against real backend):

```
POST /v1/auth/login      → 200 {token, refresh_token, expires_in:900}
POST /v1/auth/refresh    → 200 {token, refresh_token, expires_in:900}
POST /v1/auth/refresh (replay) → 401 {error: refresh_token_replayed}
GET  /v1/users/me (auth)  → 200 {id, phone, display_name}
PATCH /v1/users/me       → 200 {display_name: "Alice"}
```

## What's deferred

### Sprint 1.5 (WP-9, issue #11)

Android UI — RoomList + Room + WS extension. Out of scope for
Sprint 1. The backend protocol is stable; Android is its own
track.

### Sprint 2 (non-Sprint 1.5 backlog)

The Sprint 1 backlog is **fully closed**. Sprint 2 proper
covers:

- WS-2 / Brainstorm — Agent persona
- WP-8 — Dashboard UI (already done in commit `292e48a`; flag for
  tag cut)
- display_name modal in the dashboard login flow (the API
  supports it; the dashboard prompt is a small UI follow-up)
- The "ended-room cleanup" semantics per `keep_messages_on_end`
  (RFC §2.1 — column exists, retention worker does not)
- Per-package schema migration to physical DBs (vs. testutil's
  docker pg_dump approach)

### Sprint 1 RFC §2.3 deviations (revisit Sprint 2)

- **`hub` package is a separate package** from `ws`. The RFC
  originally had the hub as an internal implementation detail
  of `ws`. Sprint 1 chose to keep it as its own package so the
  REST `end` / `POST messages` handlers can broadcast through
  the same hub. ADR follow-up in Sprint 2.
- **No joined-room or on-stage check in the WS dispatch loop
  for `msg.send`.** The handler relies on
  `messages.CreateMessage` to enforce the on_stage check.
  Sprint 1 PR #24 acknowledged this in code comments.

### Sprint 1 Tech Debt (issue #16, all closed)

- L-1 sqlc pin landed in commit `5ab6de5` (CI is now green).
- L-2 per-package test infra landed in commit `72f7676`.
- L-3 Node 20 deprecation landed in commit `72f7676`.

## What's next (suggested)

If the reviewer signs off on the current state, the next
substantive work is either:

1. **Sprint 1.5 (WP-9 Android)** — the only remaining open issue.
   Long lead time (Android SDK + RN/Compose choice).
2. **Sprint 2 WS-2 Agent** — the user-facing agent loop. This
   is the harder design problem and the natural next IPD cycle.
3. **Sprint 1.6 housekeeping** — close the RFC §2.3 deviations
   above, add the `keep_messages_on_end` retention worker,
   finish the display_name modal in the dashboard.

## v0.3-agents 里程碑(2026-08-14)

Tagged `v0.3-agents`(agent 里程碑,已 push)。包含:

- ✅ **方式1 agent hook**(commit `a0e1b7e`,issue #38):invite/remove/mute/multi-slot/free-speech + Agent 管理器预置(openai/simple/openclaw,持久化到 gitignored 0600 `config/agents.local.json`,token write-only)。上游非 2xx 无 `error` 对象的 nil 守卫(#40)。
- ✅ **admin API**(`d85e133`):loopback-only 强关房间/清记录 + 自检页。
- ✅ **dashboard JWT `uid` claim 修复**(`d7e282f`):host 权限控制恢复。
- ✅ **方式2 设计文档**(`8ac8a18`):`docs/design/08-lobster-openclaw-hermes.md` — Hermes/OpenClaw backend driver 规划(会话路由、preset 扩展、安全、落地计划),注册进 `docs/design/00-index.md`。

### 方式2 P1(本次,未提交前 review 用)

按设计文档 §8 P1 落地最小接入(小 diff,`openai`/`simple` 行为不变):

- `presets.go`:`ProviderHermes` kind + `AgentID`/`SessionKey` 字段 + 校验(可打印 ASCII、OpenClaw 保留前缀 `subagent:`/`cron:`/`acp:` 拒绝、agent_id 仅 openclaw/hermes 可用)。
- `service.go`:`Config` 加 `AgentID`/`SessionKey`;`chat()` 会话锚定 — openclaw 走 `user: "conv:<key>"`,hermes 走 `X-Hermes-Session-Id` header(仅配 API key 时);`providerEndpoint` hermes → `/chat/completions`,openclaw 识别 gateway 面(`/v1/chat/completions`)并复用 `parseOpenAIResponse`;`replyToRoom` 默认生成 `roomID:slot` 会话(跨房隔离)。
- 测试:hermes 会话 header + agent 模型选择、openclaw gateway `conv:` 会话、validation/roundtrip、dashboard hermes 预置往返(router_test)。

验证:本地 Postgres(5432)`go test -race -count=1 -p 1 ./...` 全绿(无 docker 时 testutil 回退需 `-p 1`);`golangci-lint run ./...` 0 issues(既有 `ai_config.go:82` S1017 非本次引入)。

## Open invitations

The reviewer's three Sprint 1.5 push commits have been accepted
into this branch as commit `8040ead` and the corresponding ten
issues (#27 through #36) are now closed on the GitHub side.

For the next round, the natural follow-ups are:

- **Sprint 1.5 (WP-9 Android)** — the only remaining open issue
  (`#11`). The backend protocol is stable; Android is its own
  track.
- **Sprint 2 WS-2 Agent** — the user-facing agent loop. This is
  the harder design problem and the natural next IPD cycle.
- **Sprint 1.6 housekeeping** — close the RFC §2.3 deviations,
  add the `keep_messages_on_end` retention worker, finish the
  display_name modal in the dashboard login flow.
- **Run CI under reviewer's per-package DB testutil** to confirm
  the reviewer's `discoverContainer` resolves the right container
  on the GitHub Actions runner (the local fallback path is only
  exercised when docker / PG container are unavailable, which is
  not the case on CI).
