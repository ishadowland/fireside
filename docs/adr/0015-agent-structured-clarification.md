# ADR-0015: Agent 结构化澄清 (agent pauses a round to ask the room a structured question)

- **Status**: Accepted
- **Date**: 2026-07-31
- **Source**: borrowed from DeepTutor chat-workspace (`ask_user`); docs/design/03-protocol.md; docs/design/04-state-machines.md

## Context

Fireside 的核心场景是「需求澄清 / 情况汇报 / 联合审查」——agent 经常会缺信息。现状下 agent 只能基于已有消息流猜测后作答,猜错就产生一轮无效往返。DeepTutor 的 `ask_user` 展示了更好的做法:agent 可以**暂停本轮**,向用户抛一个结构化的澄清问题(选项 / 自由文本),收到答复后再继续。

## Decision

Agent 在单次被触发的一轮内,允许**至多一次**发出结构化澄清,把本轮挂起等待答案:

- **新系统帧 `agent.question`**(S→C):payload `{question_id, kind: single_choice|multiple_choice|text, prompt, options[]?, target: everyone|specific_participant_id?}`。渲染为房间内的一条系统消息,参与者异步作答。
- **新客户端帧 `agent.answer`**(C→S):payload `{question_id, choice_ids[]?|text}`。服务器校验:房间存在、提问 agent 是 on_stage participant、`question_id` 有对应的 pending 提问。
- **答案落到消息流**(普通消息,`ContentType=answer`,reply_to=question 消息),而不是私聊——保证圆桌可见、可归档。
- **服务器只跟踪 pending 状态**,不阻塞房间:其他人照常发言;只有该 agent 的 driver 在等答案。answer 到达后服务器回调 driver 的 continuation,driver 带着答案继续本轮。
- **超时与清理**:房间配置 `question_timeout`(默认 6h)。超时后服务器关闭 pending,并向 agent 投递一个"无答案"事件,由 agent 决定补一轮或放弃。`room.ended` / agent 下麦会**级联取消**该 agent 所有 pending 提问。
- 同一房间同一 agent **同时只允许 1 个 pending 提问**(避免刷屏,复用 ADR-0008 的防刷精神)。

## Alternatives Considered

- **agent 用普通文本提问,靠人来理解**:拒绝——没有结构化 payload,答案无法程序化路由回 driver。
- **同步阻塞整个房间等答案**:拒绝——违反 D5 异步优先。
- **开私聊通道问 host**:拒绝——答案者不一定只有 host,且偏离圆桌的公开可审计原则。

## Consequences

### Positive
- 补齐"缺信息时该怎么办"的闭环,直接服务需求澄清场景。
- 异步模型下结构化提问天然契合——答案可以晚几小时到达,不卡住任何人。
- 答案进消息流,可归档、可被其它 agent 读到。

### Negative
- 协议新增 2 种帧 + 消息 `ContentType` 新增 `question` / `answer` 两种取值。
- 需要为每个 Agent driver 增加 `Ask()` / `ResumeAfterAnswer()` 生命周期,状态机多一个 `awaiting_clarification` 态。

### Risks
- **孤儿提问**:agent 超时/崩溃/下麦后提问无人接。缓解:超时 + 级联取消 + driver 必须处理"无答案"事件。
- **答案到达时 agent 已不在房间**:服务器丢弃答案并记录日志,不投递。
- **滥用刷屏**:单 agent 单 pending 上限 + 每轮最多一次,超出按 `rate_limited` 处理。

## Related

- ADR-0008 (agent dual-trigger,防刷)
- ADR-0009 (placeholder for long responses — 提问前通常先发占位)
- docs/design/03-protocol.md(帧目录,需新增 agent.question/agent.answer)
- docs/design/04-state-machines.md(Agent Driver 状态机,新增 awaiting_clarification)
