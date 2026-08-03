# macOS Go 主进程与 Swift 原生触点收敛方案

## 1. 目标

本方案将当前 `DJOneHubNotifier` 从独立常驻进程改为 Go 主程序内的 macOS 原生用户界面模块。

目标是：

- 只运行一个 DJOneHub 主进程和一个 LaunchAgent。
- Go 统一负责设备访问、状态轮询、业务规则、去重、事件和命令执行。
- Swift 继续负责 macOS 用户触点：UserNotifications 系统通知、可选的 AppKit 自绘提示面板、菜单栏、MapKit GPS 面板、通知动作和原生窗口生命周期。
- Swift 不再访问 `127.0.0.1`，不再重复轮询，也不再直接承担通知业务判断。
- 保留当前来电、短信、未接来电、GPS、4G 菜单栏和拒接交互体验。
- 非 macOS 平台继续编译和运行，不引入 AppKit、MapKit 或 UserNotifications 依赖。

本方案的“合并”指合并进程、运行时和业务所有权，不要求把 Swift 文件机械地翻译为 Go，也不要求把所有平台代码放在同一个 Go 包中。

## 2. 当前问题

### 2.1 两个常驻进程

当前 Go 主程序负责设备和 HTTP 服务，`DJOneHubNotifier` 通过 LaunchAgent 单独启动。Swift 助手再通过 HTTP 读取来电、短信、GPS 和网络状态。

这导致：

- 来电和短信状态被两个进程分别维护。
- Swift 自己维护 `seenMessageIDs`、`seenCallHistoryIDs` 和轮询并发锁。
- 业务状态变化必须先经过 localhost HTTP，再由 Swift 重新解释。
- 主程序、通知助手和安装脚本需要同时维护端口、路由和生命周期。
- Go API 已迁移到 `/api/v1`，Swift 仍使用旧的 `/api/...` 路径，存在运行时脱节风险。

### 2.2 Go 已有可复用的基础

以下能力已经位于 Go 主程序内：

- `internal/app.App` 统一组装设备、短信、网络、GPS 和通话服务。
- `internal/application/extras` 已负责来电和 GPS 轮询。
- `internal/runtime.EventBus` 已提供进程内事件广播。
- `internal/api/http` 已提供版本化 REST API 和 WebSocket 事件流。
- `internal/platform/darwin` 已使用 cgo/libusb，具备继续增加 macOS 原生桥接的构建基础。

因此迁移重点不是重写设备能力，而是补齐事件契约、原生 UI 进程内接口和 macOS 打包方式。

## 3. 目标架构

```text
                     一个进程
┌──────────────────────────────────────────────────────────────┐
│ cmd/djonehub                                                │
│                                                              │
│  application services                                        │
│  device / SMS / call / GPS / network                         │
│              │                                               │
│              ▼                                               │
│  runtime.EventBus + notification policy                      │
│              │                                               │
│              ▼                                               │
│  platform/darwin/native bridge                               │
│       SwiftUI / AppKit / MapKit / UserNotifications           │
│              │                                               │
│              ▼                                               │
│  macOS 用户触点                                               │
│  系统通知 / 菜单栏图标 / GPS 面板 / 原生操作回调              │
└──────────────────────────────────────────────────────────────┘
```

### 3.1 Go 的职责

Go 是唯一的业务和设备运行时所有者：

- 发现、连接、重连和关闭设备。
- 读取和解析来电、短信、GPS、信号和网络状态。
- 统一管理 AT 资源锁和轮询生命周期。
- 将状态变化转换为版本化事件。
- 执行通知去重、启动时基线初始化和离线错误策略。
- 执行拒接电话、GPS 操作和其他设备命令。
- 保存需要跨界传递的 DTO，不向 Swift 暴露底层 backend 或 AT 类型。

Go 不应直接生成 SwiftUI 布局，也不应在共享 application/domain 代码中引用 AppKit。

### 3.2 Swift 的职责

Swift 是 macOS 用户触点层，不再是业务服务：

- 创建和维护 `NSApplication` 生命周期。
- 按设置用 `UserNotifications` 或 AppKit 自绘面板展示来电、短信、未接来电和错误提示，并注册系统通知动作。
- 用 AppKit 创建菜单栏状态项、窗口和点击事件。
- 用 MapKit 渲染 GPS 地图和位置标记。
- 播放 macOS 原生提示音。
- 将用户点击转换为原生桥接命令，例如 `rejectCall`、`openDashboard`、`toggleGPSPanel`。
- 将窗口关闭、菜单栏状态和用户动作回传给 Go。

Swift 不应负责：

- HTTP 请求和 JSON API 客户端。
- 来电或短信轮询。
- 新消息判断和去重。
- 设备状态缓存。
- AT 命令或设备访问。
- 端口、LaunchAgent 或业务级重试策略。

## 4. 原生桥接形式

### 4.1 推荐：Go 进程内加载 Swift 原生层

将当前 SwiftUI/AppKit/MapKit 代码改造成可被 Go 调用的 macOS 原生模块，最终链接到 Go 生成的 macOS 可执行文件中。

推荐边界如下：

```text
Go
  NativeUI.Start(config, eventCallback)
  NativeUI.Dispatch(command)
  NativeUI.Stop()

Swift
  NativeUIHost.start(...)
  NativeUIHost.receiveEvent(...)
  NativeUIHost.handleUserAction(...)
  NativeUIHost.stop()
```

Go 与 Swift 之间只传递稳定的 JSON 或 C ABI DTO，不传递 Go 指针、Swift 对象或设备 backend 对象。

建议优先采用以下实现方式之一，并在 POC 阶段验证：

1. Swift 编译为 macOS 原生静态库或 framework，导出 Objective-C 兼容的 C 接口，Go 通过 cgo 调用。
2. 在 `internal/platform/darwin/native` 中维护最小 `.h/.m` 桥接层，再由 Objective-C 调用 Swift 导出的入口。
3. 如果 Swift 静态库与 Go 链接在当前工具链上不稳定，则保留 SwiftUI 代码，用 Objective-C/AppKit 桥接重写宿主入口；业务仍全部留在 Go。

最终选择必须满足：不启动第二个 `DJOneHubNotifier` 进程、不通过 localhost 回调、不向共享 Go 包引入 Apple framework。

### 4.2 主线程约束

所有 AppKit、SwiftUI、MapKit 操作必须在 macOS 主线程执行。

- Go 启动原生层时，原生层负责建立 `NSApplication` 和主运行循环。
- Go 的设备事件不能直接从后台 goroutine 修改 Swift UI。
- 桥接层应将 Go 事件投递到主线程，再调用 SwiftUI 状态模型。
- Swift 的按钮回调不得直接执行阻塞 AT 命令；应将命令回传 Go，由 Go 异步执行。
- `Stop` 必须等待 UI 事件循环退出，避免退出时访问已释放的 AppKit 对象。

### 4.3 用户触点选择

通知入口默认使用 macOS `UserNotifications`，每类通知都可以在 Web 设置中切换为可选的 AppKit 自绘面板。系统模式由 macOS 负责圆角、材质、阴影、位置、通知中心留存和权限；自绘模式用于需要更像来电卡片的交互。macOS 普通 App 不公开 FaceTime/电话式 CallKit 通话界面，因此自绘面板是视觉方案，不是系统电话 UI。

| 用户触点 | 实现归属 | 说明 |
| --- | --- | --- |
| 来电提示 | UserNotifications / AppKit | 设置可选；自绘面板提供号码、拒接和打开控制台 |
| 短信提示 | UserNotifications / AppKit | 设置可选；自绘面板显示发送方和正文摘要 |
| 未接来电 | UserNotifications / AppKit | 使用 Go 产生的 `call.missed` 事件，设置可选 |
| 离线提示 | UserNotifications / AppKit | 使用 Go 产生的 `device.offline` 事件，设置可选 |
| GPS 菜单栏图标 | AppKit/Swift | Go 提供 GPS 状态，Swift 绘制图标 |
| GPS 地图面板 | MapKit/Swift | Go 提供坐标、HDOP 和卫星数 |
| 4G 菜单栏图标 | AppKit/Swift | Go 提供网络和信号状态 |

## 5. Go 事件契约

现有通用事件封套继续使用：

```json
{
  "id": 42,
  "type": "call.incoming",
  "version": 1,
  "occurred_at": "2026-08-02T10:00:00Z",
  "data": {}
}
```

### 5.1 首期事件

```text
device.status.changed
device.offline
call.incoming
call.updated
call.ended
call.missed
sms.received
sms.updated
gps.updated
network.updated
```

事件应携带已经脱离 backend 的 application DTO：

```json
{
  "type": "call.incoming",
  "version": 1,
  "data": {
    "id": "call-123",
    "direction": "incoming",
    "state": "incoming",
    "number": "18900007376",
    "started_at": "2026-08-02T10:00:00Z"
  }
}
```

```json
{
  "type": "sms.received",
  "version": 1,
  "data": {
    "sender": "10086",
    "body": "您的验证码是 482913",
    "code": "482913",
    "received_at": "2026-08-02T10:00:00Z",
    "index": 7
  }
}
```

### 5.2 事件产生位置

- `extras.Service.applyCalls` 在状态由非来电变为来电时发布 `call.incoming`。
- 通话结束时根据最终状态发布 `call.missed` 或 `call.ended`。
- 短信服务完成读取、长短信重组和验证码识别后发布 `sms.received`。
- 设备运行时进入离线/降级状态时发布 `device.offline`。
- GPS 和网络服务在缓存状态变化后发布 `gps.updated`、`network.updated`。
- WebSocket 和 macOS 原生 UI 均订阅同一个 EventBus，不各自实现轮询。

事件发布必须发生在服务状态更新完成后，并且不能在持有服务互斥锁时阻塞等待 UI。

### 5.3 通知策略

新增 Go `notification` application service，负责：

- 订阅事件并决定是否显示用户提示。
- 维护进程内的事件基线和去重键。
- 启动时把当前已有来电历史和短信列表设为已见，不重复提醒旧数据。
- 同一来电只提醒一次；通话结束后清理活动来电状态。
- 新短信只提醒一次，去重键沿用 `sender + recipient + body + received_at`。
- 连续设备错误达到阈值后显示一次离线提示，恢复后允许再次提示。
- 将 GPS、4G 状态变化转换为菜单栏模型更新，而不是弹出普通通知。

通知策略不依赖 Swift 是否可见，也不依赖 HTTP 端口是否启动。

## 6. 原生命令契约

Swift 只向 Go 发送有限的用户动作：

```text
reject_call { call_id }
open_dashboard
toggle_gps_panel
open_gps_panel
close_gps_panel
```

Go 返回命令结果事件：

```text
call.reject.started
call.reject.succeeded
call.reject.failed
dashboard.opened
```

拒接流程：

1. Swift 点击“拒接”。
2. Swift 发送 `reject_call`，立即进入按钮禁用状态。
3. Go 调用 `Extras.Reject`，继续使用现有 AT 资源锁和错误处理。
4. Go 发布成功或失败事件。
5. Swift 根据结果撤回来电通知或显示系统错误通知。

打开管理页面由 Go 统一生成当前管理 URL，Swift 只负责调用 `NSWorkspace`；如果桥接层不提供该能力，可以由 Go 的 macOS adapter 调用 `open` 命令。

## 7. 代码目录调整

建议目标结构如下：

```text
internal/
├── application/
│   └── notification/
│       ├── service.go          # 事件订阅、去重、通知策略
│       ├── model.go            # 原生 UI DTO 和命令 DTO
│       └── service_test.go
├── platform/
│   └── darwin/
│       └── native/
│           ├── bridge.go       # Go 侧接口和生命周期
│           ├── bridge_darwin.go
│           ├── bridge_stub.go  # 非 darwin 或无 cgo
│           ├── bridge.h
│           ├── bridge.m
│           └── Sources/         # SwiftUI/AppKit/MapKit 源码
└── app/
    └── app.go                  # 组装并启动 notification/native service
```

现有 `macos/DJOneHubNotifier` 在迁移期间可以暂时作为 Swift 源码来源，但不再保留独立的 `Package.swift` 可执行产品和独立 LaunchAgent。

建议逐步拆分当前 Swift 文件：

- `APIModels.swift`：删除 HTTP API 客户端，改为 bridge DTO。
- `AppMain.swift`：保留 macOS 生命周期和事件分发，删除所有轮询逻辑。
- `NativeNotificationService.swift`：注册系统通知分类、提交通知和处理通知撤回。
- `GPSMapPanel.swift`：基本保留，改为接收 Go GPS DTO。
- `NotificationText.swift`：保留号码和短信摘要格式化。

## 8. 启动和生命周期

### 8.1 启动顺序

1. `cmd/djonehub` 创建 `app.App`。
2. Go 启动 Runtime、设备服务、短信/通话/GPS 服务。
3. macOS 平台创建 `NativeUIHost`。
4. `notification.Service` 订阅 EventBus。
5. Native UI 请求当前快照，或接收初始化快照事件。
6. Go 启动 HTTP server。

原生 UI 启动失败不能导致设备服务和 REST API 退出。Go 应记录错误并使用无 UI 实现继续运行；macOS 发行版可以将该错误显示在日志或管理页面中。

### 8.2 关闭顺序

1. 收到 SIGINT、SIGTERM 或 AppKit 退出事件。
2. 停止接受新的原生 UI 命令。
3. 取消通知订阅和后台轮询。
4. 停止原生 UI 主循环。
5. 停止 HTTP server。
6. 关闭 Runtime 和设备 backend。

所有关闭操作必须可重复调用，避免 LaunchAgent 重启时产生悬挂窗口或未释放 USB 句柄。

## 9. HTTP/API 处理

迁移后 macOS 原生 UI 不应依赖 HTTP，但 Web 管理页面继续使用 `/api/v1`。

迁移第一阶段必须先统一现有契约：

- 将 Swift 旧路径全部移除，而不是继续维护 `/api/...` 兼容路径。
- 新事件消费者使用 `Runtime.Events()`，不通过 `/api/v1/events/ws` 回环连接自己。
- `/api/v1/calls`、`/api/v1/sms`、`/api/v1/gps` 继续服务网页和外部本地客户端。
- API DTO 和原生 UI DTO 可以共享 application model，但不得让 HTTP handler 成为原生 UI 的中间层。
- 端口只由 Go 主程序配置；Swift 不再保存默认端口。

如果需要第三方本地客户端兼容旧接口，应另行建立过渡期限和适配层，不能让原生 UI 继续依赖旧接口。

## 10. 构建和打包改造

### 10.1 目标产物

macOS 发行包只包含一个应用入口：

```text
DJOneHub.app/
└── Contents/
    ├── MacOS/djonehub
    ├── Resources/
    └── Info.plist
```

如果仍需要可执行文件供终端调试，可以从同一个 Go 构建产物复制或提供命令行参数，不再复制第二个 notifier app。

### 10.2 脚本调整

- `scripts/build-macos.sh`：在 Go 构建时链接 macOS 原生桥接，并生成带 `Info.plist` 的 `.app` 测试产物。
- `scripts/build-dmg.sh`、`scripts/build-dmg-universal.sh`：删除单独的 Swift executable、`lipo` 和 `DJOneHubNotifier.app` 组装步骤，改为打包统一 `DJOneHub.app`。
- `scripts/dmg/安装 DJOneHub.command`：删除 `NOTIFIER_DIR`、通知助手复制和第二个 plist，只安装一个应用和一个 LaunchAgent。
- `scripts/dmg/卸载 DJOneHub.command`：删除 `com.jamie.djonehub-notifier` 的 bootout 和 plist 清理逻辑。
- `macos/DJOneHubNotifier/com.jamie.djonehub-notifier.plist`：迁移完成后删除。
- `Info.plist`：将 `LSUIElement`、最小 macOS 版本、Bundle Identifier 和原生权限配置合并到统一 App。
- LaunchAgent 必须继续注册在 `gui/$UID`，并使用适合 AppKit 的交互进程类型；不能把需要菜单栏和窗口的进程注册成系统级 daemon。

### 10.3 Universal 构建

必须分别构建 arm64 和 x86_64 的 Go + 原生桥接产物，再使用 `lipo` 合并最终可执行文件。Swift 触点代码不能只验证 arm64。

构建验证至少包括：

```sh
go test ./...
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build ./cmd/djonehub
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build ./cmd/djonehub
swift build -c release
codesign --verify --deep --strict DJOneHub.app
plutil -lint DJOneHub.app/Contents/Info.plist
```

最终项目可以不再执行独立 Swift executable 的 `--self-test`，但应将展示模型测试、桥接生命周期测试和 Go 通知策略测试保留下来。

## 11. 迁移阶段

### 阶段 0：冻结契约

- 确定原生 UI 事件和命令 DTO。
- 确定通话、短信、设备离线和 GPS 事件语义。
- 记录当前启动时忽略旧消息、来电去重和错误提示行为。
- 为每个事件补 JSON fixture 和版本号。

验收：不修改用户体验的情况下，Go 可以用测试事件驱动一个假的原生 UI。

### 阶段 1：Go 内部通知服务

- 新增 `internal/application/notification`。
- 将 Swift 的轮询基线、去重和错误阈值迁移到 Go。
- 为 `extras.Service` 和 `sms.Service` 增加完整事件发布。
- 优先让 WebSocket 和单元测试消费新事件。

验收：关闭 Swift notifier 后，Go 仍能记录每个应提醒事件，且不会重复。

### 阶段 2：Swift 改为原生 View Host

- 删除 Swift HTTP client、Timer 和 API models 中的网络逻辑。
- 保留 AppKit/MapKit GPS 面板。
- 增加 C ABI 或 Objective-C 兼容的启动、事件和命令入口。
- 使用 fake bridge event 验证系统通知、通知动作、菜单栏和 GPS 面板。

验收：Swift UI 能被 Go 事件驱动，所有按钮回调能返回 Go。

### 阶段 3：单进程联调

- Go 启动 NativeUIHost。
- 禁止启动旧 `DJOneHubNotifier` LaunchAgent。
- 将拒接、打开页面、GPS 菜单栏和 4G 菜单栏全部改为进程内调用。
- 进行设备拔出、重连、通话状态变化和短信到达测试。

验收：进程列表中只存在一个 DJOneHub 应用进程，功能和旧版本一致。

### 阶段 4：打包和清理

- 合并 App bundle、Info.plist、签名和 LaunchAgent。
- 更新 DMG 安装、卸载、README 和 MACOS 文档。
- 删除旧 notifier executable、Swift Package 可执行入口和旧 API 客户端。
- 清理旧路径、旧端口说明和独立日志文件。

验收：arm64 和 universal 安装包均只安装一个 App、一个 LaunchAgent、一个日志入口。

## 12. 回滚方案

迁移期间保留一个仅供开发调试的旧 Swift notifier 构建目标，但不纳入默认发行包。

出现以下问题时可回滚到独立助手：

- Swift 静态库或 Objective-C 桥接无法稳定链接。
- AppKit 主线程或 MapKit 生命周期导致主程序不稳定。
- 原生 UI 事件丢失且无法在规定时间内修复。
- 真实硬件上的短信、来电或 GPS 回归失败。

回滚时只切换 UI host，不复制业务轮询逻辑。旧助手若临时启用，必须使用当前 `/api/v1` 契约，并明确标记为兼容模式。

## 13. 测试计划

### Go 单元测试

- 来电状态变化到 `call.incoming`、`call.missed` 和 `call.ended` 的映射。
- 短信长短信重组、验证码提取和去重。
- 启动基线不产生旧消息通知。
- 设备离线错误阈值和恢复后的再次提示。
- 原生命令参数校验和异步结果事件。
- 非 macOS stub 不调用 AppKit，Linux/Windows 测试不需要 Swift 或 cgo UI。

### Swift/UI 测试

- 事件 DTO 解码。
- 通知文本、通知分类和通知动作映射。
- GPS 无定位、定位成功、信号变化和菜单栏状态。
- 用户动作只产生预期 command，不直接执行设备操作。
- UI 在主线程更新，重复启动和关闭安全。

### macOS 集成测试

- 没有硬件时 `-demo` 能启动一个 App 并显示预览状态。
- 真实来电在系统通知和自绘面板两种模式下都能显示，拒接后对应提示正确关闭或撤回。
- 新短信只显示一次，重启后不重复显示已有短信。
- GPS 启动、搜索超时、定位成功和停止状态正确。
- 模块拔出时显示离线提示，重新连接后状态恢复。
- LaunchAgent 重启不会产生第二个进程或残留菜单栏项。

## 14. 完成定义

本方案完成时必须满足：

- 默认发行包不再包含独立 `DJOneHubNotifier.app`。
- 默认只启动一个 DJOneHub App 进程和一个 LaunchAgent。
- Go 是来电、短信、GPS、网络和错误通知的唯一业务来源。
- Swift 仍提供现有 macOS 原生用户触点和交互体验。
- Swift 不访问设备、不轮询 HTTP、不维护业务去重状态。
- 主程序不依赖自己的 HTTP 端口来驱动原生 UI。
- `/api/v1` 和 EventBus 仍可供 Vue 页面与本地客户端使用。
- arm64、x86_64 和 universal 构建均经过编译、签名和 UI 回归验证。

## 15. 推荐实施顺序

```text
事件契约
  -> Go 通知服务
  -> Swift View Host 桥接
  -> 单进程联调
  -> App bundle 与 LaunchAgent 合并
  -> 删除独立 notifier 和旧 HTTP 客户端
```

不建议先重写 Swift UI。先把业务事件和命令边界固定下来，再替换宿主和打包方式，可以最大限度保留当前用户体验并降低迁移风险。
