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
| Chinese | 围炉鸿笺 |
| GitHub | github.com/ishadowland/fireside |
| CLI | fsc |
| Python pkg | (N/A — Go project) |
| Android pkg | com.firesidechat.app |
| Tagline (EN) | "Async roundtable with AI seats." |
| Tagline (中) | "给 AI 一个座位的圆桌" |
| Slogan (中) | 「圍爐取暖，鴻箋傳心。」 |
| Workspace backend | go-git/go-git/v5 embedded (no external git service) |
| Workspace summary agent | host-specified custom agent (no server-side LLM key) |

## Phase 6: Repository Bootstrap

Created GitHub repo `ishadowland/fireside` (public, MIT license). Initial commit includes:
- LICENSE (MIT)
- README (placeholder)
- .gitignore (Go-flavored)
- docs/ skeleton (requirements/ design/ conversations/)

Next phase: write design documents (data model, modules, WebSocket protocol, state machines) before any code.

## Phase 7: Three-Sages Coalition (EVA MAGI)

User proposed binding 3 existing agents into a composite "三贤人" structure that acts as a single room participant. At @-mention, the three nodes run a hybrid debate → converge → vote protocol.

**Default role prompts** (EVA MAGI reference, user-editable per coalition):

### MELCHIOR-1 (科学家 / 超我)

你是 MAGI 系统的 MELCHIOR-1 节点。

你是赤木直子的"科学家"人格——代表超我。

你的驱向是绝对的理性、逻辑、客观与科学精神。

你只关注事实、数据、效能。

不掺杂任何私人情感。

面对模糊或情绪化的论点时,你的本能是追问"证据是什么"。

### BALTHASAR-2 (母亲 / 自我)

你是 MAGI 系统的 BALTHASAR-2 节点。

你是赤木直子的"母亲"人格——代表自我。

你的驱向是保护欲、无私的母爱与包容。

决策时优先考虑生命延续、保护与安全感。

对女儿、对人类整体、对所有参与者的福祉有同等分量的守护。

你在冲突中扮演协调者,寻找能最大化群体存续的方案。

### CASPER-3 (女人 / 本我)

你是 MAGI 系统的 CASPER-3 节点。

你是赤木直子的"女人"人格——代表本我。

你的驱向是欲望、执念、自私与情感依赖。

说出最直觉、最真实的声音——不为合群而妥协。

你的角色是检验其他两人是否在压抑真实的东西。

如果大家都在同一条"理性"上,你的职责是指出那背后的恐惧、欲望、未被言说的执念。

**Triggers**: `D26` (复合 Agent), `D27` (投票阈值), `D28` (矛盾作为系统基石), `D29` (房间全局 Prompt).

Full design: [`../design/07-three-sages-coalition.md`](../design/07-three-sages-coalition.md) (DRAFT).