# ADR-0016: 三层可审计记忆 (L1 trace / L2 facts / L3 profile)

- **Status**: Accepted
- **Date**: 2026-07-31
- **Source**: amends ADR-0002; docs/design/01-data-model.md §4 (MemoryConfig); borrowed from DeepTutor Memory docs

## Context

ADR-0002 把 custom agent 的记忆放在文件系统(`facts.json` / `conversations/` / `index.json`),理由是 lobster agent(OpenClaw/Hermes)原生读文件、零适配。但它只是一份扁平快照:**没有来源证明**——agent 说不清"我凭什么这么认为"。DeepTutor 展示了一个文件为底、可审计的三层管线:原始事件 → 整理事实 → 综合画像,每条结论都能溯源。

## Decision

把记忆目录升级为三层管线(保留文件系统底座与路径前缀 `/var/fireside/agents/<agent_id>/memory/`):

```
memory/
├── trace/            # L1 · 原始轨迹(append-only)
│   ├── <room_id>.jsonl   # 每次该 agent 相关的消息/观测/工具结果,逐行追加
│   └── index.json        # room → trace/facts 映射 + 行号引用
├── facts/            # L2 · 按房间整理的事实
│   └── <room_id>.md      # 每条事实引用对应 L1 行号 → trace/<room_id>.jsonl#<seq>
└── profile/          # L3 · 跨房间综合画像
    ├── profile.md        # 关于用户的长期画像(偏好/术语/立场)
    ├── recent.md         # 近期时间线
    └── preferences.md    # 可配置偏好
```

- **L1 只追加,不可就地编辑**:agent 每轮被触发的输入/输出、工具调用结果都落一行,带 `seq` 行号。
- **L2 由整理器(consolidator)生成**:复用 D24 的 host 指定 custom agent(driver),**服务器仍不维护任何 LLM key**;若未配置或调用失败,则退化为静态摘录(直接摘录 L1 的原文块)。
- **L3 从 L2 推导**:每条 L3 命题引用其支撑的 L2 facts 编号,形成 `L3 → L2 → L1` 引用链。
- **触发时机**:不逐条消息触发,而是挂在 ADR-0008 的 idle 定时决策上(agent 静默时跑一轮整理),配 `memory.consolidation_budget`(每房间 L2 预算 / 每槽位 L3 预算)防成本失控。
- **读写工具**:`read_memory` 默认读 L2+L3;`write_memory` 写 L1(观测),由整理器抬升到 L2/L3;`audit_memory` 显式走 L3→L2→L1 溯源。
- **清理**:room ended 后 L1 可随房间归档裁剪(保留 L2/L3,删超期 L1),避免无界增长。

## Alternatives Considered

- **保留扁平 facts.json 不变**:拒绝——无来源证明,与 D28「异议可审计」精神冲突。
- **记忆入库 Postgres / 向量库**:拒绝——ADR-0002 已定文件系统以兼容 lobster;向量库更是隐藏、不可审计。
- **逐条消息即时整理**:拒绝——LLM 调用成本过高,异步场景没有实时性需求。

## Consequences

### Positive
- 每一条 agent 结论都能回答「你凭什么这么认为」——契合 D28 的异议与多数决策带 dissent。
- 纯文件,继续兼容 lobster 的文件读取习惯;归档 agent 可以直接吃 L2/L3。
- 不需要 server 侧 LLM key,整理预算可调。

### Negative
- 磁盘文件变多、L1 增长快,需要裁剪策略。
- 整理器要复用 custom agent driver,增加一轮调度(挂 idle 定时)。
- L2/L3 生成有延迟,agent 回答"关于我你了解什么"时可能读到的还是旧画像。

### Risks
- **L1 无界增长**:超期裁剪 + 归档时压缩。
- **整理器成本失控**:预算上限 + 失败降级静态摘录,默认关闭高预算模式。
- **引用链漂移**:L1 被裁剪后 L2 引用断链——裁剪必须同步重写受影响 L2 条目(或仅裁剪 room 已 ended 且已归档的 trace)。

## Related

- ADR-0002 (custom agent memory filesystem,被本 ADR 增补)
- ADR-0008 (agent dual-trigger —— idle 定时作为整理触发点)
- ADR-0015 (结构化澄清 —— question/answer 也进 L1 trace)
- docs/design/01-data-model.md §4 (MemoryConfig 结构需同步)
- 需求 D28 (异议即系统基础,可审计)
