# Fireside 决策摘要(快照)

> 2026-07-26 全天需求澄清 + 设计产出汇总
> 用途:在 12 个开放问题拍板前,给你完整回顾当前已锁定/待决策的所有事项。

---

## 一、项目定位(已锁定)

| 项 | 决定 |
|---|---|
| 名称 | **Fireside** |
| 中文副名 | **围炉鸿笺** |
| Slogan | 「圍爐取暖,鴻箋傳心。」 |
| 标语(EN) | Async roundtable with AI seats. |
| 标语(中) | 给 AI 一个座位的圆桌 |
| 场景 | 小团队协作,Clubhouse 式异步圆桌会议 |
| 商业模式 | MIT 开源,不做商业化 |
| 仓库 | github.com/ishadowland/fireside |

---

## 二、核心模型(已锁定)

### 大厅 + 房间二级结构
- **大厅**:人类登录后停留,可举手等主持人批准
- **房间**:异步会话容器,host 独有,可拉人/拉 agent

### 三类 Agent(房间级别)
- **tool**:可调用函数,无独立人格,无 on_stage 概念
- **custom**:自定义人格 prompt + 持久 memory,完整 participant
- **lobster**:接入 Hermes/OpenClaw,完整工具栈,默认信任(MVP)

### Participant 单一状态
- `on_stage` = 在房间里(接收消息)
- `off_stage` = 已离房(不再接收)
- **进出房间 = 上下麦**,同一概念
- **热插拔**:运行时进出,host 可重新 pull

### Agent 参与模式
- 主动模式(silent / active 二选一,config 时定)
- context mode:full history / incremental(per agent 配置)

---

## 三、房间规则(已锁定)

| 规则 | 决定 |
|---|---|
| 主持人权限 | 拉/踢参与者、结束/转让房间。**禁止编辑消息** |
| 房间生命周期 | 永不过期(直到 host 主动 end) |
| 单房间容量 | ≤ 50 个 Participant |
| 多房间并行 | 同一人可同时在多房间 on_stage |
| 同步模型 | **异步优先**,不需要同时在线 |
| 历史策略 | 房间 ended 时消息清除;纪要 agent 可在清除前归档 |
| DMInRoom | MVP=false |

---

## 四、Agent 三类工具能力(已锁定)

| 决策 | 状态 |
|---|---|
| **MCP 集成**(龙虾 agent) | ✅ 保留,MVP 实现 |
| **持久 memory**(custom agent) | ⏳ 待决(01-Q2) |
| **可视化 agent 编排** | ❌ 推迟 Phase 2 |
| **AI moderator 决策流** | ❌ 不做 |
| **工作区协作 MD 文档** | ✅ 简易版已锁定 |
| **git 后端选型** | ✅ go-git 内嵌库(Gitness 已弃用) |

---

## 五、Workspace 设计(已锁定 W1-W3)

| 决策 | 锁定 |
|---|---|
| **W1 范围** | 房间级(每房间独立 repo,ended 后归档) |
| **W2 触发** | 智能定时器:agent workspace 静默 30s + ≥1 unmerged |
| **W2.2 静默定义** | 只看 agent workspace commit,人类聊天不重置 |
| **W3 摘要** | 复用 host 指定的 custom agent |
| **服务端 LLM** | ❌ 不维护 server 侧 LLM API key |

**实现栈**:`go-git/go-git/v5` + `sergi/go-diff` + `yuin/goldmark`,**0 外部服务**

**Room.Config 新增字段**:
- `WorkspaceEnabled bool`
- `WorkspaceAutoMergeSeconds int`(默认 30,0=纯手动)
- `WorkspaceSummaryAgentID string`

**Workspace 状态机**:initialized → active → merging_git → merging_summary → merged(→ 循环 active) / conflict / archived

---

## 六、技术栈(已锁定)

```
语言:    Go 1.22+
Web:     Gin
SQL:     sqlc(类型安全 SQL 生成,类似 MyBatis)
DB:      Postgres 15+
迁移:    golang-migrate
WS:      gorilla/websocket
校验:    go-playground/validator
日志:    log/slog(标准库)
CLI:     cobra(fsc)
部署:    systemd + Nginx + Let's Encrypt
```

**关键不引入项**:
- ❌ Redis(MVP 单进程足够)
- ❌ ORM(Hibernate 风格)— 用 sqlc 写原生 SQL
- ❌ 独立 git 服务(Gitness 已死,go-git 内嵌)
- ❌ LLM API key(diff 摘要走 agent driver)

---

## 七、安全(已锁定)

| 项 | 决定 |
|---|---|
| MVP 单租户 | Docker 多实例扩展 |
| 传输加密 | TLS 1.3 强制(Nginx 终结) |
| 应用层 E2E | Phase 2(与纪要 agent 可读性冲突,先搁置) |
| 龙虾 agent 安全 | MVP 信任模型,Phase 2 接 Linux namespace |
| 鉴权 | 手机号验证码 + JWT |
| 用户标识 | E.164 手机号(唯一) |

---

## 八、决策编号清单(D1-D25)

| # | 主题 | 决定 | 日期 |
|---|---|---|---|
| D1 | 场景 | 小团队协作,Clubhouse 式 | 2026-07-26 |
| D2 | 主持人权限 | 拉/踢/结束,**禁编辑** | 2026-07-26 |
| D3 | 房间生命周期 | 永不过期,≤50 人 | 2026-07-26 |
| D4 | 多房间并行 | 允许 | 2026-07-26 |
| D5 | 同步模型 | **异步优先** | 2026-07-26 |
| D6 | 历史 | ended 清空;纪要 agent 归档 | 2026-07-26 |
| D7 | Agent 模式 | silent / active 二选一 | 2026-07-26 |
| D8 | context mode | full / incremental 可配 | 2026-07-26 |
| D9 | tool agent | 无 on_stage,纯可调用 | 2026-07-26 |
| D10 | 媒体 | MVP 文字+图片;Phase 2 语音 | 2026-07-26 |
| D11 | 算力 | VPS 纯编排,agent 外部运行 | 2026-07-26 |
| D12 | 租户 | MVP 单租户,Docker 多实例 | 2026-07-26 |
| D13 | 商业模式 | MIT 开源 | 2026-07-26 |
| D14 | 大厅 | 举手 + 主持人批准 | 2026-07-26 |
| D15 | 加密 | TLS 1.3;E2E Phase 2 | 2026-07-26 |
| D16 | 项目名 | Fireside / 围炉鸿笺 | 2026-07-26 |
| D17 | CLI 名 | fsc | 2026-07-26 |
| D18 | 技术栈 | Go + Gin + sqlc + Postgres + gorilla/ws | 2026-07-26 |
| D19 | 纪要 agent | 读全量 → 结构化 Markdown → 清原始 | 2026-07-26 |
| D20 | 龙虾安全 | MVP 信任 | 2026-07-26 |
| D21 | 改名 | 围炉夜话 → 围炉鸿笺 + slogan | 2026-07-26 |
| D22 | Workspace 范围 | 房间级 | 2026-07-26 |
| D23 | Workspace 自动合并 | 智能定时(agent 静默 30s + ≥1 unmerged) | 2026-07-26 |
| D24 | Workspace 摘要 | 复用 custom agent,server 无 LLM key | 2026-07-26 |
| D25 | git 后端 | go-git 内嵌(Gitness 已弃) | 2026-07-26 |
| D26 | **Three-Sages (EVA MAGI)** | Composite agent `kind='coalition'` 绑 3 个 agent 为 MELCHIOR / BALTHASAR / CASPER(EVA Magi 三节点:科学家/超我 + 母亲/自我 + 女人/本我)。默认 role prompts 随仓库发布,用户可改。@ 提及触发 R1(辩论)→ R2(收敛)→ R3(投票)→ Synth 4 轮协议,对外呈现为单个 participant | 2026-07-31 |
| D27 | **投票阈值** | 常态 2:1 多数决;**极端 3:0 一致**。极端触发:节点打 `extreme` flag / 不可逆操作关键词正则(delete, ban, self-destruct, revoke, force-quit,可配)/ host 显式打 `critical` / room config 强制一致 | 2026-07-31 |
| D28 | **矛盾作为系统基石** | 不追求"唯一最优解"。三节点分歧 = 正常态 ≠ 故障。多数决 2:1 必须**带保留异见**汇报(不能漂白成 3:0)。极端决策 3:0 不达成 → 系统正确行为是**挂起 + 请求 host 人工介入**,不走多数决兜底(参 EVA *Air*: CASPER 投 No,自爆失败) | 2026-07-31 |
| D29 | **Room 全局公告(announcement)** | `rooms.announcement TEXT ≤500 chars`(默认空)。host/admin 可改,其他只读。每个 agent prompt 拼装顺序:`[room announcement] + [agent role prompt] + [agent persona prompt] + [user message]`。UI:房间顶部 sticky bar + ✏️ 编辑按钮。辩论中途改 announcement → 当前轮用旧值,下一轮起切换新值(不重滚) | 2026-07-31 |

---

## 九、当前待决策 12 个开放问题

### 01 数据模型(3)
- **Q1**:Tool agent handler 是 webhook 还是消息总线?
- **Q2**:Custom agent memory 存 DB 还是文件系统?
- **Q3**:RaiseHand 是否需要超时自动拒绝?

### 02 模块划分(3)
- **Q1**:HTTP + WS 单端口还是双端口?
- **Q2**:Android 端是否做离线消息缓存?
- **Q3**:Tool agent 同步调用还是异步触发?

### 03 WebSocket 协议(3)
- **Q1**:WS 鉴权用 query string 还是首个 frame?
- **Q2**:Active agent 插话决策触发时机?(新消息后 vs 定时轮询)
- **Q3**:Agent 长响应是否 stream token?

### 04 状态机(3)
- **Q1**:新房间能否引用老房间 ID 作"父话题"?
- **Q2**:hand.raise 24h 无活动是否自动清?
- **Q3**:Driver shutdown 立即还是延后?

---

## 十、当前仓库文档结构

```
fireside/
├── README.md
├── LICENSE (MIT)
├── .gitignore
├── docs/
│   ├── requirements/
│   │   ├── 00-overview.md          (D1-D25 决策表)
│   │   ├── 01-tech-decisions.md    (Go vs Python + 栈选型)
│   │   └── 02-conversation-summary.md  (今天对话精编)
│   ├── design/
│   │   ├── 00-index.md
│   │   ├── 01-data-model.md        (7 实体 + Workspace + 索引)
│   │   ├── 02-modules.md           (Go 后端 + Android 包结构)
│   │   ├── 03-protocol.md          (WS 帧 + 路由 + 错误码)
│   │   └── 04-state-machines.md    (Room/Participant/Workspace 生命周期)
│   └── conversations/
│       ├── README.md
│       └── 2026-07-26-01-original-request.md
```

---

## 十一、当前 commit 历史

```
3d6a0dc design: lock W1-W3 decisions (room-scoped, smart timer, agent-summary)
68fd0c9 design: add Workspace (shared MD + git version control)
8493bbc design: clarify stage state = in/out-of-room (single state, not two-layer)
73a8179 design: remove '阅后即焚/ephemeral' phrasing per user preference
fea6a4f design: 4 foundational design docs (v0.1 draft)
9d5691f docs: rename 围炉夜话 → 围炉鸿笺; add slogan
b3db0e3 Bootstrap: docs skeleton + requirements overview
56bcf26 Initial commit
```

---

## 十二、下一步行动

**12 个开放问题等你拍板**。拍板方式选一种:
- **逐条答**:按 01-Q1 / 01-Q2 / ... 一条条回
- **批量**:你说"按推荐走"我按各章节推荐方案处理
- **跳过**:直接进入编码,代码里遇到再回头补决策

这份快照用途:
- 你回顾当天决定用
- 编码时遇到模糊处查阅用
- 后续 commit message 引用 D 编号追溯决策用