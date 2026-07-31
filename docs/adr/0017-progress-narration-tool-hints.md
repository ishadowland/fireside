# ADR-0017: 进度叙述 + 工具线索 (progressive narration replaces the static placeholder)

- **Status**: Accepted
- **Date**: 2026-07-31
- **Source**: amends ADR-0009; docs/design/02-modules.md (Q3); borrowed from DeepTutor chat-workspace / partners (`Send Progress`, `Send Tool Hints`)

## Context

ADR-0009 规定长响应先写一条静态占位消息("⏳ <agent> 正在思考..."),完成后替换。问题是:长轮次里用户盯着一条静态占位,不知道 agent 在干什么,IM 场景下沉默超过一分钟还会被当成"坏了"。DeepTutor 的 IM 伙伴会在慢轮次投递进度叙述,并用一行式工具线索(如 `rag(query=…)`)交代动作。

## Decision

把"一条静态占位"升级为**渐进式进度叙述**,最终答复仍走 ADR-0009 的删除-新建模式:

- **新系统消息 `agent.progress`**(S→C):一轮内 agent 可发 0..N 条,payload `{round_id, seq, text}`。`text` 是自然语言进度("正在检索 workspace…")。
- **工具线索**:同一条 `agent.progress` 可附带 `tool_hint` 字段(`rag(query=…)` / `workspace.merge` 等一行式描述),由房间开关控制。
- **两个房间级开关**(`RoomConfig`,对齐 DeepTutor 的渠道开关语义):
  - `SendProgress bool`(默认 true)——是否投递中间进度;
  - `SendToolHints bool`(默认 false,安静模式)——是否投递一行式工具调用线索,生产环境建议关。
- **客户端渲染**:progress 消息在 UI 里**折叠在占位消息/最终回复之下**,作为可展开的「步骤」组(活动轨迹);IM 渲染为普通小消息。最终回复到达后占位按 ADR-0009 软删,progress 消息**保留**为轨迹(不随占位删除),room ended 时清理。
- **顺序保证**:progress 带 `round_id + seq`;driver 在完成最终答复前不得发与最后一条 progress 乱序的帧(由 driver 输出顺序保证,服务器仅校验 seq 单调)。

## Alternatives Considered

- **保持单条静态占位不变**:拒绝——长时间无反馈,用户无法判断 agent 是否活着。
- **流式 token(streaming)**:拒绝——ADR-0009 已判定 MVP 异步场景收益低、成本高。
- **只发工具线索、不发进度**:拒绝——线索对普通用户不可读,两者拆开关更灵活。

## Consequences

### Positive
- 长轮次"活着"的信号;工具调用对用户透明、可审计(与 ADR-0016 的 L1 trace 互补)。
- 开关化,可针对 IM / 生产安静模式调优。

### Negative
- 新增系统消息类型 `agent.progress` + 2 个 RoomConfig 字段。
- Agent driver 需要多实现一个 `EmitProgress()` 回调(可与 ADR-0015 的 Ask 共用一套输出通道)。

### Risks
- **进度刷屏**:折叠渲染 + 默认发送但工具线索默认关 + 每轮 progress 数量上限(建议 ≤10,超出截断)。
- **顺序竞态**:seq 单调校验;丢帧由客户端按 seq 重排。
- **泄露内部信息**:工具线索可能暴露内部函数名——默认关,或由 agent 自行决定是否给出可读版本。

## Related

- ADR-0009 (placeholder for long responses,被本 ADR 增补)
- ADR-0006 (tool agent async placeholder)
- ADR-0015 (结构化澄清 —— 提问前的占位/进度同通道)
- docs/design/03-protocol.md(帧目录,需新增 agent.progress)
- docs/design/02-modules.md(并发模型:agent 输出通道)
