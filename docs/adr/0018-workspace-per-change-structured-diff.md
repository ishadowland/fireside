# ADR-0018: 工作区合并输出「逐处变更」结构化 diff (surgical, per-hunk)

- **Status**: Accepted
- **Date**: 2026-07-31
- **Source**: amends D23/D24; docs/design/01-data-model.md §8 (MergeDiffSummary); borrowed from DeepTutor Co-Writer

## Context

设计文档里 workspace 合并(D23 智能定时触发)产出的 diff 摘要(D24 摘要 agent)是**聚合统计**——`total_files_changed / total_additions / total_deletions / per_branch_commits`,外加一段 AI 通读。对「联合审查」场景来说,聚合数字不可审:**看不出某个具体改动是谁、为什么做的**。DeepTutor 的 Co-Writer 展示了外科手术式编辑的价值:改动按**选中片段**锚定、逐处呈现,不重写整篇文档。

## Decision

合并的产出从"聚合统计 + 一段通读摘要"升级为**逐处变更(hunk)级的结构化 diff**,MVP 不做人工 accept/reject 门禁(异步场景会卡住 D23 自动合并):

- **分层 diff**:`WorkspaceService.Merge` 用 `sergi/go-diff` 把文件级差异拆成 hunks,每个 hunk 结构化为 `{file, change_type: insert|modify|delete, before/after 摘录, source_branch}`。
- **摘要 agent 按 hunk 注解**(D24 不变,仍复用 host 指定的 custom agent,服务器无 LLM key):prompt 改为"对每个 hunk 给一句『为什么改、对应哪个需求点』";未配置或调用失败 → 退化为静态 per-hunk 统计(改动行数、文件、来源分支)。
- **合并通知消息列出 per-hunk 条目**,可折叠,不再是纯聚合数字;`workspace.state`(event=merged)的 `merge_summary` 同步带上 hunks 列表。
- **门槛**:`MergeDiffSummary` 增 `Hunks []MergeHunk`;超大 diff(如 hunks > 50)截断显示并标注 `truncated=true`,聚合统计仍完整。
- **不改 agent 写入模型**:agent 仍在各自 branch 写完整文件,不做"选区级写入"(那是 Co-Writer 在同步编辑器里的能力,与异步 branch 模型冲突)。

## Alternatives Considered

- **全量 accept/reject diff 门禁**:拒绝——异步自动合并(D23)会被人为等待阻塞,MVP 不做。
- **维持纯聚合摘要**:拒绝——不可审,联合审查场景价值低。
- **强制按选区写入**:拒绝——需要同步编辑协议,与分支协作模型不兼容。

## Consequences

### Positive
- 审查可落到具体改动;异议/追问有锚点(符合 D28 的联合审查基调)。
- 摘要 agent 的产出更有结构,喂给归档 agent(L2/L3)更顺。
- 改动统计仍保留,降级路径简单。

### Negative
- `MergeDiffSummary` 结构变大;merge 消息体变大(需折叠渲染)。
- 摘要 agent 调用成本上升(按 hunk 注解);用门槛 + 截断控制。
- `sergi/go-diff` 需按 hunks 精细切分,比"整体 diff 统计"多一点实现复杂度。

### Risks
- **大仓库/hunk 爆炸**:上限截断 + `truncated` 标记。
- **摘要 agent 对单 hunk 产生低质量注解**:降级静态统计 + 归档保留原始 hunks 可复查。
- **与 D23 定时器竞态**(合并进行中又有新 commit):沿用现有 `merging_git` 状态串行化,本 ADR 不改变合并时序。

## Related

- 需求 D23 (workspace 自动合并 —— 触发时序不变)
- 需求 D24 (workspace 摘要复用 custom agent —— 产出结构升级)
- docs/design/01-data-model.md §8 (MergeDiffSummary / WorkspaceMerge 需同步)
- docs/design/04-state-machines.md (merging_summary 状态,语义不变)
- ADR-0016 (hunk 注解可作为 L1 观测进入 agent 记忆)
