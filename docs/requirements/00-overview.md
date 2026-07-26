# Requirements Overview

> 需求总览 — 2026-07-26 需求澄清产出

## Origin

User wanted to use **Hermes Agent** in multi-party discussions (需求澄清 / 情况汇报 etc.) inside **WeChat groups**. Direct integration proved infeasible (Hermes WeChat adapter uses iLink Bot identity which cannot receive ordinary WeChat group events). This shifted the requirement from "integrate with existing IM" to "build a dedicated platform".

## One-line Definition

A **Clubhouse-style async roundtable** where a host can pull in humans and AI agents into a persistent room for multi-party discussion.

## Core Model

```
┌──────────────────────────────────────────────────────────┐
│  Platform                                                 │
│  ├── Lobby (大厅)         — humans only, raise-hand queue │
│  └── Room  (房间)         — host-owned, async container  │
│      ├── Humans           — pulled in by host             │
│      └── AI Agents        — server-configured, host pulls │
│          ├── Tool agent   — callable, no persona          │
│          ├── Custom agent — persona + persistent memory   │
│          └── Lobster      — full Hermes/OpenClaw stack    │
└──────────────────────────────────────────────────────────┘
```

## Frozen Decisions

| # | Decision | Choice | Date |
|---|---|---|---|
| D1 | Scene | Small team collaboration, Clubhouse-style | 2026-07-26 |
| D2 | Host permission | Pull/remove participants, end/transfer room. **No message editing.** | 2026-07-26 |
| D3 | Room lifecycle | Persistent until host ends. Max 50 participants per room. | 2026-07-26 |
| D4 | Multi-room | Same human can be on-stage in multiple rooms simultaneously. | 2026-07-26 |
| D5 | Sync model | **Async-first.** Rooms are persistent; participants need not be online concurrently. | 2026-07-26 |
| D6 | History | Default **ephemeral** — server clears messages when room ends. Archive agent can be triggered to summarize. | 2026-07-26 |
| D7 | Agent participation mode | Each agent config: silent (only respond to @) or active (poll + insert). Humans default to active. | 2026-07-26 |
| D8 | Agent context mode (on-stage) | Configurable per agent: full history / incremental since on-stage | 2026-07-26 |
| D9 | Tool agents | No on-stage concept; callable functions. | 2026-07-26 |
| D10 | Media | MVP: text + image. Phase 2: voice ASR/TTS. Agent multi-modal flag in config. | 2026-07-26 |
| D11 | Compute | VPS is pure coordinator. **All LLM compute runs in external agents.** | 2026-07-26 |
| D12 | Tenancy | Single-tenant MVP. Multi-tenant via Docker containers on VPS. | 2026-07-26 |
| D13 | Business model | Open-source (MIT). No commercialization planned. | 2026-07-26 |
| D14 | Lobby | Humans land in lobby post-login; raise-hand with optional note; host approves/rejects. | 2026-07-26 |
| D15 | Encryption | TLS 1.3 transport minimum. Application-layer E2E deferred to Phase 2 (conflicts with archive agent readability). | 2026-07-26 |
| D16 | Project name | **Fireside** / 围炉鸿笺 | 2026-07-26 |
| D17 | CLI name | `fsc` | 2026-07-26 |
| D18 | Tech stack | Go 1.22+ / Gin / sqlc / Postgres / gorilla/websocket / golang-migrate / cobra / slog | 2026-07-26 |
| D19 | Archive agent | When triggered, reads full history, emits structured markdown summary with timestamps + speaker map. Original messages cleared post-archive. | 2026-07-26 |
| D20 | Lobster security | MVP: trust model. Phase 2: Linux namespace isolation. | 2026-07-26 |
| D21 | Name revision | Renamed 围炉夜话 → **围炉鸿笺**; added slogan **「圍爐取暖，鴻箋傳心。」**. Historical archive (`docs/conversations/2026-07-26-01-original-request.md`) retains original wording for fidelity. | 2026-07-26 |
| D22 | Workspace scope | Per-room (not tenant-wide). Each room optionally mounts one workspace, archived with the room. | 2026-07-26 |
| D23 | Workspace auto-merge | Smart timer — triggers when `workspace_auto_merge_seconds` (default 30) elapse since last agent commit AND ≥1 branch HasUnmerged. Human chat does NOT reset the timer. `0` disables auto-merge (manual only). | 2026-07-26 |
| D24 | Workspace diff summary | Reuse host-specified custom agent (no server-side LLM API key required). Falls back to static diff stats if no agent configured or agent call fails. | 2026-07-26 |
| D25 | Git backend | `go-git/go-git/v5` embedded library (NOT Gitness — that project was archived/absorbed into Harness commercial). No external git service deployed. | 2026-07-26 |
| D26 | Three-Sages (EVA MAGI) | Composite agent `kind='coalition'` binding 3 existing agents as MELCHIOR / BALTHASAR / CASPER (EVA Magi 三节点). Default role prompts shipped; user-editable. Renders as single room participant; @-mention triggers 3-round hybrid protocol. | 2026-07-26 |
| D27 | Voting threshold | Normal: 2:1 majority. Extreme: 3:0 unanimous. Extreme triggers: any node `extreme` flag, irreversible-action keyword regex (configurable), host/admin explicit `critical` tag. | 2026-07-26 |
| D28 | Conflict as system foundation | Do not pursue single "optimal" answer. Three-node disagreement is normal, not failure. Majority decisions are reported **with** the dissent (2:1 not laundered into 3:0). Extreme-decision 3:0 deadlock → correct behavior is **suspend and request human intervention**, not majority fall-through. | 2026-07-26 |
| D29 | Room global prompt (announcement) | `rooms.announcement TEXT` (≤500 chars, default empty). Host/admin editable, others read-only. Injected at top of every agent prompt: `[room announcement] + [agent role prompt] + [agent persona prompt] + [user message]`. UI: sticky bar at room top + ✏️ edit button. Live debate does not re-roll on announcement change; affected rounds use the new value from the next round onward. | 2026-07-26 |

## Open / Deferred Items

- **Application-layer E2E encryption** (Phase 2; pending design for archive agent visibility)
- **Keyword-trigger routing** for active agents (Phase 2; cost model TBD)
- **Multi-tenant UI** (solved by Docker; out of MVP scope)
- **Voice/video** (Phase 2+)
- **Lobby "super host" concept** (frequent collaborators skip raise-hand) — pending decision

## Out of Scope (MVP)

- Voice/video calls
- File sharing (only images MVP)
- Full-text search across archived rooms
- Plugin marketplace
- Agent training / fine-tuning

## See Also

For a consolidated snapshot of all decisions to date (with open questions and architecture choices), see:

- **[`03-decision-snapshot.md`](./03-decision-snapshot.md)** — Single-document summary, ideal for review before coding begins