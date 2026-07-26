# Conversation Summary

> 2026-07-26 Hermes Agent 集成微信群聊需求澄清全流程

## Phase 1: Original Request

User asked: 把 Hermes Agent 拉到某个微信群聊进行三方对话。

## Phase 2: WeChat Feasibility Check

Investigation findings:
- Hermes has built-in WeChat adapter via **iLink Bot API** (`ilinkai.weixin.qq.com`)
- User has prior login session (`742eb9070852@im.bot`, token saved 2026-04-20)
- **Critical limitation**: iLink bot identity cannot receive ordinary WeChat group events
- Documented warning: `WEIXIN_GROUP_POLICY=disabled` is default; even `open` does not work for bot-type accounts

**Conclusion**: Native WeChat integration cannot support real-time group discussion. User agreed to drop this approach.

## Phase 3: Alternative Platforms Considered

Considered in order:
1. **Telegram/Discord groups with multiple bots** — rejected (unreliable in mainland China)
2. **DingTalk groups** — feasible but limited Markdown rendering
3. **Feishu topics + multiple agents** — feasible, user already uses Feishu
4. **Self-hosted web room** — rejected by user (login/UI/security concerns)
5. **WeChat mini-program** — rejected (requires ICP license,审核 hard for individuals)
6. **Android app + VPS backend** — **SELECTED**

## Phase 4: Requirements Clarification

After multiple rounds of Q&A, locked decisions documented in `00-overview.md`.

Key Q&A thread:
- Scene: small team, Clubhouse-style async
- Multi-party composition: humans + 3 agent types (tool / custom / lobster)
- Agent participation modes (silent/active)
- On-stage context mode (full history / incremental)
- History policy (ephemeral + archive agent)
- Encryption (TLS MVP, E2E Phase 2)
- Tech stack evolution: Python → Go (due to VPS resource constraints)

## Phase 5: Naming

Long naming discussion. User proposed `firesideTP` (Tea Party) — agent flagged political sensitivity concerns. User agreed to revert to plain `Fireside`.

Final naming matrix:

| Slot | Value |
|---|---|
| Project | Fireside |
| Chinese | 围炉夜话 |
| GitHub | github.com/ishadowland/fireside |
| CLI | fsc |
| Python pkg | (N/A — Go project) |
| Android pkg | com.firesidechat.app |
| Tagline (EN) | "Async roundtable with AI seats." |
| Tagline (中) | "给 AI 一个座位的圆桌" |

## Phase 6: Repository Bootstrap

Created GitHub repo `ishadowland/fireside` (public, MIT license). Initial commit includes:
- LICENSE (MIT)
- README (placeholder)
- .gitignore (Go-flavored)
- docs/ skeleton (requirements/ design/ conversations/)

Next phase: write design documents (data model, modules, WebSocket protocol, state machines) before any code.