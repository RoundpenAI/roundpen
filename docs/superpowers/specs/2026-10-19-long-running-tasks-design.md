# 长时间任务（分段续跑）设计

日期：2026-10-19  
状态：Phase 1 落地中  
范围：常驻任务 / Project Worker / 环境 TTL；**不做**单次 Prompt 挂 12 小时。

## 1. 问题

复杂工作可能要连续数小时甚至超过 12 小时。今天的硬墙：

| 路径 | 上限 |
|------|------|
| 常驻任务 `max_duration_sec` | 原 1h，现升至 **2h/段** |
| 对话一轮 `turnTimeout` | 2h |
| Project worker | 原 45min，现 **2h** |
| Agent/Browser 沙箱 TTL | 创建 24h；工具活动会 Touch |

单次 `Prompt` 拉到 12h 会撞上下文截断、配额、槽位独占和控制面重启。成熟做法是**分段**。

## 2. 模型

```
长任务（一件事）
 └─ Segment 1（≤2h Prompt）→ UpdateRoutineState(checkpoint) → FinishRun(continue=true)
 └─ Segment 2（新会话，读同一 state）→ … → FinishRun(continue=true)
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

## 5. 非目标（后续）

- ACP `ResumeSession` / 同会话断点恢复  
- 单 Prompt 12h、按 token 预算  
- 多槽并行长任务  
- 凭据库与浏览器登录态（仍卡抢券/报税类）
