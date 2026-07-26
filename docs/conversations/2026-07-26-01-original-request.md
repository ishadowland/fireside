# Conversation: 2026-07-26 — Initial Request & Requirements Clarification

## Summary

User requested integrating Hermes Agent into a WeChat group for three-way discussion. Investigation revealed WeChat iLink Bot identity cannot receive group events, so this approach was abandoned. User pivoted to designing a dedicated platform (Fireside). Through multiple Q&A rounds, requirements were frozen, tech stack was decided (Go on VPS), and the project was bootstrapped on GitHub.

## Key Decisions Made This Session

1. Dropped direct WeChat integration (iLink bot identity limitation)
2. Dropped Telegram/Discord (mainland China unreliability)
3. Chose Android app + VPS backend architecture
4. Adopted Clubhouse-style async roundtable model with lobby + rooms
5. Defined 3 agent types (tool / custom / lobster)
6. Defined agent participation modes (silent / active)
7. Defined ephemeral history policy with archive agent
8. Single-tenant MVP, Docker for multi-tenant future
9. MIT licensed, open-source project
10. **Go 1.22+** stack (after comparing Go vs Python for VPS resource constraints)
11. **Fireside** as final project name (after rejecting `firesideTP` due to political sensitivity)

## Conversation Excerpts

> Note: below is a curated digest of key exchanges, not verbatim transcript.

### 1. Original Request

**User**: 我需要把 HERMES AGENT 拉到某个微信群聊 进行三方对话, 这个需求如何实现

**Agent**: Investigated Hermes WeChat adapter (`ilinkai.weixin.qq.com`); identified critical limitation that iLink bot identity cannot receive ordinary WeChat group events. Provided alternative paths (WeCom / DM relay / platform switch).

### 2. Pivot to Dedicated Platform

**User**: 都不太好

**Agent**: Listed real options (A: WeCom, B: DM relay, C: platform switch) with honest trade-offs.

**User**: 能否通过一个轻量化的对话APP或者小程序,实现三方或者几方讨论,(需求澄清\情况汇报等) 可以拉HERMES AGENT\ 人类或者其他AGENT进行讨论? 这个从技术上能否实现

**Agent**: Affirmed feasibility; outlined 3 implementation granularities (Feishu topic, mini-program, dedicated app).

### 3. Constraints Discussion

**User**: 小程序或者APP方案比较合适, 自建WEB房间的登录\用户管理和网络安全比较存疑

**Agent**: Validated concern; presented mini-program vs APP trade-offs; pivoted to **Android APP + VPS backend** approach.

**User**: 安卓 APP 方案可以, 不需要审核. 我已经有一个带域名的VPS,可以运行服务端

**Agent**: Drew architecture diagram, listed tech stack options.

### 4. Tech Stack Evolution

**User**: 先不用开始编码 我需要先把所有的需求澄清完

**Agent**: Generated 9-point requirements clarification checklist (core scenario, participant capabilities, routing intelligence, context persistence, async/sync mode, media types, compute location, tenancy, business model).

**User**: Responded with detailed requirements covering all 9 points. **Notable decision: async-first sync model.**

**Agent**: Generated MVP scope (Phase 1) and follow-up questions on WeChat iLink, encryption, project naming.

### 5. Naming Iteration

**User**: Fireside 我喜欢, 中文可以用围炉夜话之类的名字. 请增加一个英文词或者缩写,让名字独特性更高,避免撞车

**Agent**: Listed 5 candidates (Fireside Chat, Fireside Sync, Fireside Agora, Fireside Loop, Fireside Hub). Recommended `Fireside Chat` with `fsc` CLI.

**User**: firesideTP 怎么样

**Agent**: Flagged that TP = Tea Party has political sensitivity concerns in US context, plus ambiguity issues. Recommended single-name `Fireside`.

**User**: TP缩写 Tea Party

**Agent**: Reiterated concerns; clarified that brand distinctiveness comes from logo + tagline + community, not abbreviation. Recommended final: `Fireside`.

**User**: 好 用回 fireside

**Agent**: Locked naming matrix; froze tech stack.

### 6. Language Choice: Go

**User**: 用golang是否比python性能更好? vps的硬件资源很少, 需要严格控制性能开销

**Agent**: Demonstrated Go's advantage on low-resource VPS (10x memory efficiency, native goroutine fit for WebSocket). Provided comparison table; recommended Go 1.22+ stack (Gin / sqlc / Postgres / gorilla/websocket).

**User**: 好的 ,切go

**Agent**: Froze Go stack; previewed upcoming architecture documents.

### 7. Repository Creation

**User**: 先别开始写代码.你先去gh上创一个新的项目仓库,新建一个目录,用于存档需求澄清过程和产出的设计文档.

**Agent**: Created `github.com/ishadowland/fireside` (public, MIT, Go-flavored .gitignore, README skeleton, docs/ structure).

**User**: 请把今天我们的对话内容和产出文档归档进去

**Agent** (this file): Archiving conversation summary + requirements overview + tech decisions into the new repo.

## Artifacts Created This Session

- GitHub repo: `github.com/ishadowland/fireside`
- Files: `README.md`, `LICENSE` (MIT), `.gitignore`
- Docs: `docs/requirements/00-overview.md`, `docs/requirements/01-tech-decisions.md`, `docs/requirements/02-conversation-summary.md`
- Docs: `docs/design/00-index.md`
- Docs: `docs/conversations/README.md`, `docs/conversations/2026-07-26-01-original-request.md` (this file)

## Next Steps (post-session)

1. Populate `docs/design/01-data-model.md` (entities + ER diagram + DB schema)
2. Populate `docs/design/02-modules.md` (backend / Android / shared boundaries)
3. Populate `docs/design/03-protocol.md` (WebSocket message frames + routing)
4. Populate `docs/design/04-state-machines.md` (Room / Participant / Agent lifecycle)
5. User review and approval
6. Then begin Phase 1 coding