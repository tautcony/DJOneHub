## Context

2026-08-11 完成上游 vOWiFi 功能同步：旧的 7 态 `Host` 状态机（`internal/vowifihost/host.go`，已删除）替换为 `internal/vowifihost.Manager` + 宿主适配器（`internal/application/vowifi/host_adapter.go`），并引入期望态自动拉起。`openspec/specs/vowifi-lifecycle/spec.md` 的需求全部仍成立，但正文按旧实现叙述。

## Goals / Non-Goals

**Goals:**
- 需求 1-4 正文更新为实现载体（Manager + 适配器）的事实描述
- 新增"期望态自动拉起"需求（启动 5s + 30s 对账 + 单飞退避），反映 `service.go` 的 `desiredLoops` / `ScheduleDesiredRecover` 行为

**Non-Goals:**
- 不改动任何需求语义（现有四条需求行为不变）
- 不混入 IMS 注册器/代理/卡策略等其他功能（各自独立变更）

## Decisions

- **纯规格更新**：无代码变更；delta 采用"修改需求正文 + 新增一条需求"。
- **实现载体表述**：`Manager`（LifecycleController 按设备串行 + generation 过期拒绝 + 抢占；RuntimeStore epoch/claim 启动会话防重；DesiredRecoverStore 期望态退避；StateHub 状态发布订阅）+ 宿主适配器（把单设备运行时适配为 `vowifihost.Adapter`）。
- **期望态需求语义**：服务启动 5s 后首次自动拉起；此后 30s 低频对账；恢复失败按 30s/1m/2m 递增退避；单飞（同一设备同时只允许一次恢复）。

## Risks / Trade-offs

- 需求正文描述具体间隔（5s/30s）会增加规格对实现的耦合；作为验收可观察行为保留，间隔属实现细节可由实现调整。
