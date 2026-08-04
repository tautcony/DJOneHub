# macOS 原生桥接契约（Native Bridge Contract）

本文档冻结 Go 主进程与 macOS 原生 UI（SwiftUI/AppKit/MapKit）之间的跨进程边界契约，与 `docs/MACOS_GO_NATIVE_BRIDGE_PLAN.md` 第 5/6 节对应。契约类型位于 `internal/application/notification/model.go`，每个事件的 JSON fixture 位于 `internal/application/notification/testdata/`，由 `contract_test.go` 持续验证。

规则：

- 事件封套沿用 `runtime.Event`：`{id, type, version, occurred_at, data}`，版本由 `EventBus.Publish` 固定为 `1`。
- WebSocket 通道与 macOS 原生 UI 订阅**同一个** EventBus，不各自轮询。
- Swift 只发送有限的命令（见第 3 节），不直接访问设备、不维护去重状态；通知权限和显示偏好由 Go 暴露给本地设置页。
- 修改任何已冻结类型必须同步更新 fixture、测试和版本号。

## 1. 事件

### 1.1 事件一览

| 事件 | version | 载荷类型 | 产生位置 |
| --- | --- | --- | --- |
| `device.status.changed` | 1 | `domain.Snapshot`（既有契约，未改） | `runtime.transition` / `scan` |
| `device.offline` | 1 | `domain.OfflineEvent`（运行时发布；桥接层经通知服务转换为 `DeviceOfflineEvent`） | `runtime.transition` 进入 degraded/disconnected/absent 状态 |
| `call.incoming` | 1 | `CallEvent` | `extras.applyCalls` 非来电 → 来电（新增） |
| `call.updated` | 1 | `CallEvent` | `extras.applyCalls` 状态/号码变化（新增） |
| `call.ended` | 1 | `CallEvent` | `extras.applyCalls` 通话结束且未接（新增） |
| `call.missed` | 1 | `CallEvent` | `extras.applyCalls` 通话结束且未接（新增） |
| `sms.received` | 1 | `SMSMessageEvent` | 短信服务增量发布：首次加载只建基线，之后仅对新增消息发布（新增） |
| `sms.updated` | 1 | 既有 `{"count"}` 等 map 载荷 | 短信服务既有发布点（数量变化或首次加载时发布，避免轮询空转刷屏） |
| `network.updated` | 1 | 既有 map 载荷；新发布点使用 `NetworkUpdateEvent` | 网络服务既有发布点；新的 15 秒 radio 轮询发布 `NetworkUpdateEvent` |

### 1.2 CallEvent（call.* 事件载荷）

```json
{
  "id": "1783069200000-1",
  "direction": "incoming",
  "state": "incoming",
  "number": "18900007376",
  "started_at": "2026-08-02T10:00:00Z",
  "ended_at": "2026-08-02T10:00:45Z",
  "missed": false
}
```

| 字段 | 说明 |
| --- | --- |
| `id` | `<unix毫秒>-<CLCC index>`，与 `extras.CallRecord.ID` 一致 |
| `direction` | `incoming` / `outgoing` |
| `state` | `incoming` / `waiting` / `active` / `alerting` / `dialing` / `held` |
| `started_at` / `ended_at` | `ended_at` 仅在 `call.ended` / `call.missed` 出现 |

### 1.3 SMSMessageEvent（sms.received 载荷）

```json
{
  "index": 7,
  "sender": "10086",
  "recipient": "",
  "body": "您的验证码是 482913",
  "received_at": "2026-08-02T10:00:05Z"
}
```

去重键（与短信服务缓存键一致）：`sender \x00 recipient \x00 body \x00 received_at(RFC3339Nano)`，实现为 `SMSMessageEvent.DedupKey()`。

### 1.4 DeviceOfflineEvent（device.offline 载荷）

```json
{ "state": "disconnected", "reason": "no managed device was discovered", "last_error": "..." }
```

### 1.5 NetworkUpdateEvent（新发布点的 network.updated 载荷）

```json
{ "mode": "usbnet", "network_mode": "LTE", "registered": true, "operator": "CHN-UNICOM", "signal_dbm": -83 }
```

驱动 4G 菜单栏模型；网络模式发布点（`{"mode": ...}`）保持现状，后续逐步收敛。

## 2. 事件产生位置

- `extras.Service.applyCalls`：状态由非来电变为来电（incoming/waiting）时发布 `call.incoming`；通话结束时按最终状态发布 `call.missed` 或 `call.ended`；状态/号码变化发布 `call.updated`（仅实际变化时发布，避免 3 秒轮询刷屏）。同一通话被新通话替换时先按结束处理归档。
- 短信服务：3 秒轮询 `Refresh`（替代旧 notifier 的轮询），完成读取和长短信重组后对**新增消息**发布 `sms.received`；首次加载只建立进程内缓存基线，不发布。
- 设备运行时：`transition` 进入 degraded/disconnected/absent 时发布 `device.offline`，载荷为 `domain.OfflineEvent`。
- 网络服务：15 秒 radio 轮询（替代旧 notifier 的蜂窝轮询）发布 `network.updated`（`NetworkUpdateEvent`，变化检测）。

事件发布必须发生在服务状态更新完成之后，并且不能在持有服务互斥锁时阻塞等待 UI（发布点均在解锁后调用）。

## 3. 原生命令

Swift 只发送以下动作；Go 异步执行并发布结果事件：

| 命令 | 参数 | 结果事件 |
| --- | --- | --- |
| `reject_call` | `call_id`（必填） | `call.reject.started` / `call.reject.succeeded` / `call.reject.failed` |
| `open_dashboard` | 无 | `dashboard.opened` |

命令 JSON：

```json
{ "name": "reject_call", "params": { "call_id": "1783069200000-1" } }
```

结果事件载荷：

- `RejectResult`：`{call_id, error?}`（`failed` 必须带 `error`）。
- `DashboardOpened`：`{url}`。

拒接流程：用户在 macOS 系统通知或自绘面板中点击“拒接” → Swift 发送 `reject_call` → Go 调用 `Extras.Reject`（沿用 AT 资源锁和错误处理）→ 发布成功/失败事件 → Swift 撤回来电通知、关闭自绘面板或显示错误状态。

### 3.1 通知权限与显示偏好

通知权限状态由 Swift 查询后通过内部命令 `notification_permission_status` 回传 Go，状态包括 `not_determined`、`authorized`、`denied`、`provisional` 和 `unsupported`。设置页使用以下本地 API：

- `GET /api/v1/notifications/permissions`：读取权限状态。
- `POST /api/v1/notifications/permissions/request`：请求 macOS 授权。
- `POST /api/v1/notifications/permissions/open-settings`：打开 macOS 通知设置。
- `GET /api/v1/notifications/preferences`：读取四类通知的显示方式。
- `PUT /api/v1/notifications/preferences`：更新 `incoming_call`、`missed_call`、`sms`、`device_offline`，每项为 `system` 或 `custom`。

偏好保存于 `~/Library/Application Support/DJOneHub/notification-preferences.json`，默认全部为 `system`。`system` 使用 `UserNotifications`，`custom` 使用 AppKit 自绘面板；两种模式共享 Go 的事件、去重和拒接命令链路。

## 4. 冻结的基线行为（迁移自独立 Swift notifier）

以下行为由 Go `notification` 服务在迁移后承担，语义与现 `DJOneHubNotifier` 保持一致：

| 行为 | 语义 |
| --- | --- |
| 启动基线 | 启动时把当前已有来电历史和短信列表设为已见，不重复提醒旧数据；来电与短信基线相互独立 |
| 来电去重 | 同一来电（按 `CallRecord.ID`）只提醒一次；通话结束后清理活动来电状态 |
| 未接来电 | 历史中第一个 `missed && !seen` 的记录弹“未接来电” |
| 短信去重 | 新短信只提醒一次；去重键为 `sender + recipient + body + received_at`（第 1.3 节） |
| 离线提示 | 连续设备错误达到 5 次才显示一次离线提示；恢复后允许再次提示 |
| 4G | 状态变化转换为菜单栏模型更新，不弹普通通知 |
| 通知显示方式 | 每类通知默认使用 `UserNotifications`；设置为 `custom` 时使用 AppKit 自绘面板。来电支持拒接，短信和离线提示在自绘模式下短暂显示 |

GPS 地图、GPS 菜单栏图标和搜星动画已移除，不属于当前桥接契约。

## 5. 版本策略

- 当前所有事件版本为 `1`（`EventVersion` 常量）。
- 新增字段必须保持向后兼容（旧字段不回退、`omitempty` 可缺省）；破坏性变更必须升级版本号并更新 fixture。
- 命令名与结果事件名是稳定标识，不可改名；新增命令时追加常量。

## 6. 桥接 ABI（Go ↔ Swift）

C 接口声明在 `internal/platform/darwin/native/bridge.h`，Swift 侧以 `@_cdecl` 在 `NativeUIHost.swift` 实现，静态库由 `macos/DJOneHubNotifier` 构建：

```c
void native_ui_start(const char *config_json, native_ui_command_cb on_command, native_ui_ready_cb on_ready);  // 阻塞至 UI 退出
void native_ui_handle_event(const char *event_json);  // 任意线程可调，内部投递主线程
void native_ui_stop(void);                            // 请求 UI run loop 退出
```

线程约束：

- `native_ui_start` 必须在进程主线程调用：`cmd/djonehub` 的 main goroutine 启动即 `runtime.LockOSThread()`，桥接在主 goroutine 上同步运行 UI，HTTP 服务跑在 worker goroutine。
- Swift 侧所有 AppKit/SwiftUI/MapKit 操作在主线程；事件在 Swift 内 `DispatchQueue.main.async` 后触碰 UI。
- `on_command`（用户动作 JSON）与 `on_ready` 在 UI 主线程触发，Go 在新建 goroutine 中处理；命令经 `notification.ValidateCommand` 校验后交给 app 层的 `CommandHandler`。
- UI 就绪前到达的事件在 Swift 侧缓冲，`applicationDidFinishLaunching` 后按序投递。

Go 侧桥接 `internal/platform/darwin/native`：

- `bridge.go`：平台无关的 `Bridge`（实现 `notification.Sink` 转发策略批准的事件、命令路由、`Send` 结果事件）。
- `bridge_darwin.go`（darwin + cgo）：cgo 胶水与 `//export` 回调。
- `bridge_stub.go`（非 darwin / 无 cgo）：no-op，`HasUI()` 为 false，其他平台无 AppKit 依赖。
- 事件以 `runtime.Event` JSON 封套过桥；`cmd/djonehub` 在 `HasUI()` 时把 HTTP server 放到 goroutine，主 goroutine 阻塞于 `Bridge.Start`，UI 退出即应用退出。

## 7. 测试

- `contract_test.go`：逐条解码 `testdata/` fixture，验证封套（id/type/version/occurred_at）与载荷字段；命令 fixture 验证参数解析与校验。
- `notification` 服务单测：启动基线、去重、离线阈值、4G 转发、命令校验。
- `extras`/`sms` 事件发布测试：通话状态机到 `call.incoming/updated/ended/missed` 的映射、短信增量语义。
- Swift `BridgeEventTests`：事件 DTO 解码（含小数秒时间戳）、格式化、命令编码。
- 桥接测试：Sink 事件 JSON 转发、命令校验与分发。
- macOS 集成（阶段 3 验收）：`-demo` 单进程启动 UI 与 HTTP 共存，SIGTERM 干净退出。
