## Why

`openspec/specs/vowifi-lifecycle/spec.md` 的四条需求（运行时/服务层持有、设备变化恢复、数据面能力诚实、生命周期串行收敛）自 2026-08-11 上游 vOWiFi 同步后仍全部成立，但实现载体已从旧的 7 态 `Host` 状态机换成 `internal/vowifihost.Manager`（LifecycleController 串行化 + RuntimeStore epoch/claim 防重 + DesiredRecoverStore 单飞退避 + StateHub 状态发布）+ 宿主适配器。规格正文仍按旧实现叙述，且缺失同步后的新行为：期望态自动拉起（启动 5s + 30s 对账）。本次更新实现描述并补充该需求。

## What Changes

- 更新 `vowifi-lifecycle` 规格的需求正文，把实现载体描述从旧 `Host` 状态机改为 `Manager` + 宿主适配器（生命周期串行控制器、epoch/claim 启动会话、期望态恢复退避、状态发布订阅）。
- 新增一条需求：VoWiFi SHALL 支持期望态自动拉起——服务启动后 5s 自动拉起、此后 30s 低频对账恢复，恢复按设备退避（30s/1m/2m 递增）单飞执行，避免事件风暴下并发恢复。

## Capabilities

### New Capabilities

（无新能力）

### Modified Capabilities

- `vowifi-lifecycle`: 更新实现载体描述（Host → Manager + 适配器）；新增"期望态自动拉起（5s 启动 + 30s 对账）"需求

## Impact

- `openspec/specs/vowifi-lifecycle/spec.md`（delta 后全量更新）
- 无代码/API 变更：本变更纯规格更新，反映 2026-08-11 已完成实现（`internal/vowifihost`、`internal/application/vowifi`）
