# 长时间任务（分段续跑）设计

日期：2026-10-19  
状态：Phase 2 落地中  
范围：常驻任务 / Project Worker / 环境 TTL；**不做**单次 Prompt 挂 12 小时。

## 1. 问题

复杂工作可能要连续数小时甚至超过 12 小时。今天的硬墙：

| 路径 | 上限 |
|------|------|
| 常驻任务 `max_duration_sec` | 原 1h，现升至 **2h/段** |
| 对话一轮 `turnTimeout` | 2h |
| Project worker | 原 45min，现 **2h** |
| Agent/Browser 沙箱 TTL | 约 24h；工具活动会 Touch |

单次 `Prompt` 拉到 12h 会撞上下文截断、配额、槽位独占和控制面重启。成熟做法是**分段**。

## 2. 模型

```
长任务（一件事）
 └─ Segment 1（≤2h Prompt）→ UpdateRoutineState(checkpoint) → FinishRun(continue=true)
 └─ Segment 2（同 Roundpen 会话，读同一 state）→ … → FinishRun(continue=true)
 └─ Segment N → FinishRun(continue=false)   // 全部做完
```

- **权威进度**：`routines.state`（已有，64KB）。
- **段身份**：`routine_runs.segment_index` + `continue_from`。
- **链上限**：`MaxContinueChain = 24`（约 48h 墙钟预算）。
- 确认闸 `waiting_user` 仍暂停本段预算；不计入段链以外的魔法。

## 3. Agent 合同

1. 长活开始前把游标写入 `UpdateRoutineState`。
2. 本段时间不够或还有步骤：`FinishRun({ summary, continue: true })`。
3. 全部完成：`FinishRun({ summary, continue: false })`（默认）。
4. 下一段 prompt 会带上 state + 上一段 summary；模型从 checkpoint 接着做，不重开炉灶。

## 4. 环境

`EnsureAgent` 在槽位已 Running 时 `Touch`，避免「只思考不 Exec」的长段被 24h TTL 边缘误杀。Exec / Browser 路径原本就会续期。

## 5. Phase 2

### 5.1 同会话续跑

- Continue 段复用父 run 的 Roundpen `session_id`（不是新建 chat）。
- 进程内：`ResumeRoutine` 热重绑 `FinishRun` 的 run key + `ResumeSession` 清 cancel，ACP 会话不断。
- 进程重启后：对新 runtime `NewSession`，历史仍从 Roundpen session 回放。

### 5.2 Token 预算

- `routines.max_tokens`：整条 continue 链的软顶（0 = 不限，只靠墙钟）。
- `routine_runs.tokens_used`：本段累计；上游 usage 经 `OnTokens` 落库。
- 段启动时把剩余预算写入 Agent；触顶后提示 `FinishRun(continue=true)` 并结束本 turn。

### 5.3 多槽并行

- 交互槽 `agent` 与长任务槽 `agent-job` 分容器、同 workspace。
- 常驻任务与 project worker（implementer/reviewer/tester）走 `EnsureAgentJob`，不堵聊天。

## 6. 非目标（后续）

- 跨进程死亡的完整 ACP Resume（持久化 ACP SID）
- 单 Prompt 12h
- N 路以上自由并行槽
- 凭据库与浏览器登录态（仍卡抢券/报税类）
