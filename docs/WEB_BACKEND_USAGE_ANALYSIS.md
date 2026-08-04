# Web 到后端代码使用分析

分析日期：2026-08-04

## 1. 范围与判断标准

本报告以当前仓库根目录的 `web/` 和生产入口 `cmd/djonehub` 为起点，追踪：

```text
Vue 页面/Store -> web/src/services/api.ts 或 WebSocket
  -> internal/api/http
  -> internal/application
  -> internal/runtime / internal/backend / internal/platform / internal/modem
```

`vohive-open/` 是独立的嵌套 Go module，并且有自己的 `go.mod` 与 `web/`。它没有被根目录的 `cmd/djonehub` 导入，因此不纳入当前 Web 运行链；报告最后单独说明它。

代码状态分为四类：

| 标记 | 含义 |
|---|---|
| Web 活跃 | 当前 Vue 源码或 WebSocket 会触发，且能追到后端实现 |
| 后端已注册、当前 Web 未调用 | HTTP 可被外部客户端调用，但当前 Vue 源码没有调用 |
| 内部活跃 | 没有 REST 调用，但由启动流程、轮询器或事件总线使用 |
| 当前生产链未到达 | 文件会编译或有测试覆盖，但当前 `cmd/djonehub` 的装配路径没有到达；不能等同于源码绝对无用 |

本次环境没有提供可调用的独立 subagent 接口，因此按“设备”“短信/eSIM”“网络/VoWiFi”“通知/Extras”四个模块并行完成了等价的分组分析，并保留每组的独立证据。

## 2. 总体结论

1. 当前生产入口只有一个：`cmd/djonehub/main.go:20-74`。它创建 `app.App`，启动运行时、短信/网络/通话轮询器，然后挂载 HTTP API 和 Vue 静态资源。
2. 当前 Web 直接使用的主要业务链是设备状态、短信刷新/发送/清空、eSIM、网络状态/模式/连通性、VoWiFi 控制、通知管理、通话和 Raw AT；GPS 已从当前产品入口移除。
3. 后端另外注册了若干当前 Vue 未调用的接口：设备能力接口、短信列表接口、网络流量 REST 接口、网络诊断、操作查询、OpenAPI 文档和 `/api/v1/device` 兼容别名。这些接口仍是公共 API，不能直接删除。
4. 网络流量不是“未使用”：`network.Service` 在启动时后台轮询，并发布 `network.traffic.updated`；Vue 通过 WebSocket 读取该事件。只有对应的 REST 查询接口当前没有 Web 调用。
5. 当前生产装配只使用 `backend.NewATFactory`：`internal/app/app.go:65-83`。macOS 设备实际进入 `CommandBackend` 或 `ATBackend`，取决于设备是 USB bulk AT 还是串口 AT：`internal/platform/darwin/adapter.go:53-114`、`internal/backend/at_factory.go:24-62`。
6. `QMIBackend`、`MBIMBackend` 和 `NewBackend` 工厂目前没有被根目录生产代码调用。它们有编译价值和测试价值，但不是当前 Web 请求的运行后端。
7. 当前 AT/Command 后端并不覆盖所有已注册 Web 能力。尤其 VoWiFi 没有在这两个当前后端中暴露 `VoWiFiPort`/`VoWiFiServicePort`，串口 AT 路径也没有 `ESIMPort`/`NetworkPort`；因此对应 Web 按钮存在，但在特定设备路径上会返回能力不支持。
8. 当前工作区已经把本地持久化从单独 JSON 文件迁移到 SQLite：`internal/app/app.go:91-176` 创建数据库，`internal/application/sms` 保存外发短信，`internal/application/extras` 保存 profile notes，通知偏好也使用 SQLite namespace；旧 JSON 只用于一次性迁移。

## 3. Web 请求入口清单

### 3.1 当前 Vue 实际调用

所有普通 HTTP 请求都由 `web/src/services/api.ts:18-132` 拼接为 `/api/v1`。调用位置主要在 `web/src/App.vue` 和 `web/src/stores/device.ts`。

| Web 调用 | Vue 证据 | HTTP 入口 | 应用服务 | 当前状态 |
|---|---|---|---|---|
| 设备状态 | `web/src/stores/device.ts:27` | `GET /device/status` | `device.Service.Status` | Web 活跃 |
| 重新扫描 | `web/src/App.vue:1009` | `POST /device/actions/rescan` | `device.Service.Rescan` | Web 活跃 |
| 重启 | `web/src/App.vue:1021` | `POST /device/actions/reboot` | `device.Service.Reboot` | Web 活跃 |
| 短信刷新 | `web/src/App.vue:296,311` | `POST /sms/actions/refresh` | `sms.Service.Refresh` | Web 活跃 |
| 短信清空 | `web/src/App.vue:326` | `POST /sms/actions/clear` | `sms.Service.Clear` | Web 活跃 |
| 发送短信 | `web/src/App.vue:977` | `POST /sms/actions/send` | `sms.Service.Send` | Web 活跃 |
| eSIM 概览 | `web/src/App.vue:418` | `GET /esim` | `esim.Service.Overview` | Web 活跃 |
| eSIM 下载/启用/重命名/删除 | `web/src/App.vue:517,666-695` | 对应 eSIM action | `esim.Service` | Web 活跃 |
| 网络状态 | `web/src/App.vue:710,722` | `GET /network` | `network.Service.Status` | Web 活跃 |
| 网络模式/连通性 | `web/src/App.vue:1032,1043` | 对应 network action | `network.Service.SetMode/Check` | Web 活跃 |
| Raw AT | `web/src/App.vue:1059` | `POST /device/actions/raw-at` | `rawat.Service.Execute` | Web 活跃 |
| VoWiFi 状态/控制 | `web/src/App.vue:747,996-999` | 对应 VoWiFi 路由 | `vowifi.Service` | Web 活跃，但依赖后端能力 |
| 通知调试 | `web/src/App.vue:766,873` | `GET/POST /notifications/debug` | `notification.Service.Debug` | Web 活跃 |
| 通知权限 | `web/src/App.vue:778,812,832` | permissions 路由 | native bridge 回调 | Web 活跃 |
| 通知偏好 | `web/src/App.vue:790-850` | `GET/PUT /notifications/preferences` | bridge + SQLite namespace | Web 活跃 |
| 通话 | `web/src/App.vue` | calls 路由 | `extras.Service` | Web 活跃 |
| eSIM 健康/本地备注 | `web/src/App.vue:421-422,517-518` | health/notes 路由 | `esim.Service` + `extras.Service` | Web 活跃 |
| 实时事件 | `web/src/stores/device.ts:83-100` | `GET /events/ws` | `runtime.EventBus` | Web 活跃 |

### 3.2 API 客户端声明但当前 Vue 没有调用

以下方法存在于 `web/src/services/api.ts`，但 `rg 'api\\.<method>' web/src` 没有找到实际调用：

| 客户端方法 | 对应接口 | 后端实现 | 判断 |
|---|---|---|---|
| `api.capabilities` | `GET /device/capabilities` | `runtime.Snapshot` | API 客户端闲置；Vue 改为从 `status.snapshot.capabilities` 读取 |
| `api.sms` | `GET /sms` | `sms.Service.List` | API 客户端闲置；当前页面用 `smsRefresh` 获取列表 |
| `api.operation` | `GET /operations/{id}` | `operation.Manager.Get` | API 客户端闲置；当前页面主要通过 WebSocket 的 operation 事件更新状态 |

这些是前端 SDK 中的未使用方法，不代表后端接口无用。

## 4. 模块 A：设备、Runtime 和 WebSocket

### 4.1 调用链

```text
api.status / WebSocket snapshot
  -> http.Server.deviceStatus / http.Server.websocket
  -> device.Service.Status
  -> runtime.Snapshot + runtime.Backend
  -> ModemBackend.Identity / Radio / SIM
```

证据：路由注册在 `internal/api/http/server.go:59-100`；设备处理器在 `internal/api/http/server.go:112-145`；业务服务在 `internal/application/device/service.go:23-69`；Runtime 后端选择和状态轮询在 `internal/runtime/runtime.go:43-177`。

### 4.2 使用判定

| 代码 | 状态 | 说明 |
|---|---|---|
| `device.Service.Status` | Web 活跃 | Web 首次加载和 WebSocket 首帧都会使用 |
| `device.Service.Rescan` | Web 活跃 | 直接调用 `runtime.Rescan` |
| `device.Service.Reboot` | Web 活跃 | 通过 `Rebooter` 类型断言进入当前后端 |
| `runtime.Start/Stop/Snapshot/Events` | 内部活跃 | App 启动、HTTP 状态和 WebSocket 都依赖 |
| `runtime.consumeBackendEvents` | 内部活跃 | 把 modem、SIM、SMS、eSIM、网络、VoWiFi 事件转成统一事件 |
| `/api/v1/device` | 后端已注册、当前 Web 未调用 | 与 `/device/status` 共用 `deviceStatus`，是兼容别名 |
| `/api/v1/device/capabilities` | 后端已注册、当前 Web 未调用 | 当前 Web 从 status snapshot 取能力 |
| `operation.Manager.Get` | 后端已注册、当前 Web 未调用 | 由 operation REST 查询使用；Web 目前不轮询 |
| `operation.Manager.Cancel` | 当前生产 HTTP 链未到达 | 有实现和测试，但没有取消操作的 HTTP 路由；前端也没有取消方法 |
| `operation.Manager.Events` | 当前生产调用未发现 | 当前统一事件直接使用 Runtime 的 EventBus；保留该方法主要是接口便利性 |

## 5. 模块 B：短信和 eSIM

### 5.1 短信调用链

```text
POST /sms/actions/refresh
  -> sms.Service.Refresh
  -> RequireCapability(sms_read)
  -> ResourceAT lock
  -> ModemBackend.ListSMS
  -> Reassemble + merge
  -> sms.updated / sms.received EventBus event
```

发送链为 `POST /sms/actions/send` → `sms.Service.Send` → `ModemBackend.SendSMS`，异步任务由 `operation.Manager.Start` 执行。清空链为 `POST /sms/actions/clear` → `SMSPort.DeleteAllSMS`。

证据：`internal/api/http/server.go:158-221`；`internal/application/sms/service.go:72-124,201-282`；`internal/app/app.go:204-216`。短信服务在 App 启动时还会启动 3 秒轮询，因此 `sms.Service.Start/poller` 是内部活跃代码。

### 5.2 使用判定

| 代码 | 状态 | 说明 |
|---|---|---|
| `sms.Service.Refresh/List/Send/Clear` | Web 活跃或内部活跃 | Refresh、Send、Clear 有 Web 入口；List 被通知服务启动基线调用 |
| `sms.Service.Read` | 当前 Web 链未到达 | 没有 HTTP handler，也没有当前生产调用；`SMSPort.ReadSMS` 仍被接口适配层和后端实现保留 |
| `Reassemble`、`merge`、`recordSent` | 内部活跃 | 短信刷新、长短信合并和发送历史都会使用 |
| `GET /sms` 与 `api.sms` | 后端已注册、当前 Web 未调用 | 页面当前通过 Refresh 获取相同列表 |
| `ATBackend` 的 `ReadSMS/DeleteSMS/ListSMS/DeleteAllSMS` | 部分可达 | List/DeleteAll 由当前短信服务使用；Read/Delete 单条暂无 Web 入口 |
| `CommandBackend` 的完整 SMS PDU 处理 | Web 活跃 | macOS USB bulk AT 路径会进入该实现 |
| `storage.SQLiteStore` 外发短信表 | 内部活跃 | `app.newApp` 迁移旧历史，`sms.NewService` 读取，`sms.Service.recordSent` 写入 |

### 5.3 eSIM 调用链

```text
GET /esim 或 POST /esim/actions/*
  -> esim.Service
  -> RequireCapability(esim)
  -> backend.ESIMPort
  -> macOS CommandBackend.esimPort
  -> internal/platform/darwin/esim_port.go / internal/esim
```

概览、下载、启用、重命名、删除均有对应 Web 调用，处理器在 `internal/api/http/server.go:223-300`，服务在 `internal/application/esim/service.go:37-142`。健康检查和本地备注分别在 `server.go:451-523`，前端调用见 `App.vue:418-422,517-518`。

注意：macOS 串口 AT 路径构造的是 `Adapt(NewATBackend(m))`，`ATBackend` 没有暴露 `ESIMPort`；macOS USB bulk AT 路径的 `OpenAT` 会创建 `CommandBackend` 并尝试设置 eSIM port。因此 eSIM API 虽然从 Web 可达，实际能力取决于设备进入哪条 AT 传输路径。

## 6. 模块 C：网络和 VoWiFi

### 6.1 网络调用链

```text
GET /network
  -> network.Service.Status
  -> backend.NetworkPort.Status（如果后端支持）
  -> platform NetworkController.Status
  -> macOS network.go / Linux adapter / unsupported adapter
```

网络模式和连通性同样由 `network.Service` 选择 backend port 或平台 controller，具体代码在 `internal/application/network/service.go:112-216`。

| 代码/接口 | 状态 | 说明 |
|---|---|---|
| `network.Service.Status/SetMode/Check` | Web 活跃 | 对应当前 Web 的状态、模式、检测按钮 |
| `network.Service.Start/poller` | 内部活跃 | App 启动时运行并发布网络状态事件 |
| `network.Service.Traffic/trafficPoller` | 内部活跃 | `Start` 启动流量轮询；事件被 Vue `App.vue:641-642,728` 消费 |
| `GET /network/actions/traffic` | 后端已注册、当前 Web 未调用 | REST 入口可用，但页面使用 WebSocket 流量事件 |
| `network.Service.Diagnostics` | 后端已注册、当前 Web 未调用 | 只有诊断接口调用；实现会追加平台状态和 Raw AT 结果 |
| `transport.NetworkDiagnostics` | 通过诊断接口可达 | macOS 实现为 `internal/platform/darwin/network.go:126` |
| `transport.NetworkTrafficReader` | 内部活跃 | macOS 实现为 `network.go:62`，被流量轮询调用 |

### 6.2 当前平台后端边界

- `internal/app/app.go:70-78` 在 macOS、Linux、Windows 都传入 `backend.NewATFactory`。
- macOS 的 `darwin.Adapter` 同时提供 USB/串口发现和网络 controller；这是当前主要生产路径。
- Linux `Adapter` 实现了 sysfs 发现和网络接口状态，但如果候选只有 `ControlPath` 而没有 `ATPort`，`ATFactory` 需要 `OpenAT`，而 Linux 传入的是 `nil`，因此当前代码没有把 QMI/MBIM 控制路径接入运行时。
- Windows 使用 unsupported adapter，属于编译和平台占位路径；当前没有可用的硬件后端装配。

### 6.3 VoWiFi 调用链和实际可达性

```text
GET /vowifi 或 POST /vowifi/actions/*
  -> vowifi.Service.Status/Enable/Disable/Reconnect
  -> vowifihost.Host
  -> backend VoWiFi port + platform Network/Tunnel + SIM/APDU
```

路由和服务确实活跃，证据为 `internal/api/http/server.go:542-586`、`internal/application/vowifi/service.go:167-219`。但当前 `CommandBackend` 和 `ATBackend` 的方法清单中没有 `VoWiFiPort` 或 `VoWiFiServicePort` 实现；`CommandBackend.Capabilities` 也没有 `vowifi_inspect`/`vowifi_control`。所以当前 Web 调用通常会在 `RequireCapability` 阶段得到能力不支持。`vowifihost`、SIM auth、PacketTunnel 等代码是为有对应后端 port 的未来/其他装配保留的，不是当前默认 AT Web 链的有效硬件实现。

## 7. 模块 D：通话、通知和 Native Bridge

### 7.1 Extras

`internal/app/app.go` 在启动时创建并启动 `extras.Service`。该服务运行通话后台轮询器：

- 通话：每 3 秒调用 `AT+CLIP=1`、`AT+CLCC`，发布 `call.incoming`、`call.updated`、`call.ended` 或 `call.missed`。

HTTP 入口在 `internal/api/http/server.go`，前端调用在 `web/src/App.vue`。因此 `Calls/Reject/Notes/SaveNote` 能从当前 Web 到达；轮询和事件转换是内部活跃代码。

GPS 功能已按产品决定移除：已删除 `CapabilityGPS`、`extras` GPS 轮询和 `AT+QGPS*` 调用、`/api/v1/gps*` 路由、Web GPS 页面、`gps.updated` 桥接事件，以及 macOS GPS 地图/菜单栏 UI。历史 changelog 和设计草案中的 GPS 文字仅作为历史记录保留，不代表当前生产功能。

`extras.Service.MarshalNotes`（`internal/application/extras/service.go:537-542`）当前生产调用未发现，只有独立工具方法性质，暂判为“当前生产链未到达”，不判为可以立即删除。

### 7.2 Notification Service

通知服务由 Runtime EventBus 驱动，并由 macOS native bridge 作为 Sink：

```text
Runtime/EventBus
  -> notification.Service.Start/handle
  -> native.Bridge
  -> Swift native notifier
```

Web 还可以通过 `/notifications/debug` 注入调试事件。权限和偏好接口通过 `app.newApp` 注入的 bridge 回调访问；偏好写入 SQLite namespace，再同步给 bridge。旧 JSON 偏好只在 namespace 不存在时迁移。证据：`internal/app/app.go:107-203`、`internal/application/notification/service.go:66-170,256-399`、`internal/api/http/server.go:588-721`。

| 代码 | 状态 |
|---|---|
| `notification.Service.Start/Stop/handle` | 内部活跃 |
| `Debug`、`DebugActions` | Web 活跃 |
| 权限状态/申请/打开设置 | Web 活跃，依赖 native bridge |
| 偏好读取/写入 | Web 活跃，依赖 SQLite namespace 和 native bridge |
| `ValidateCommand` | 非 Web 但内部活跃；由 `internal/platform/darwin/native/bridge.go:281` 校验 native command |
| Swift notifier 文件 | 非 HTTP 代码，但属于通知事件链的有效消费者 |

## 8. Internal 模块全量盘点

本节以 `internal/` 当前目录为边界，判断每个 Go package 是否进入根目录生产入口 `cmd/djonehub`。包内部分文件可能仍处于“同包编译/测试可用”状态，因此表中的“部分活跃”表示包被使用，但并非所有文件或导出的函数都被当前 Web 链调用。

### 8.1 包级使用矩阵

| Internal 包 | 生产状态 | 主要生产调用者 | 未接入或限制 |
|---|---|---|---|
| `internal/app` | 活跃 | `cmd/djonehub/main.go` | 当前唯一装配根；负责 SQLite、服务和平台选择 |
| `internal/api/http` | 活跃 | `internal/app` | API 路由中仍有部分仅供外部客户端使用 |
| `internal/application/device` | 活跃 | `app`、HTTP、各业务 Service | 设备服务是 Runtime 的业务门面 |
| `internal/application/esim` | 活跃 | `app`、HTTP | 实际依赖 `backend.ESIMPort`，能力由后端决定 |
| `internal/application/extras` | 活跃 | `app`、HTTP、notification baseline | 通话轮询和 profile notes 都在此包 |
| `internal/application/network` | 活跃 | `app`、HTTP | 流量/状态轮询是内部事件链 |
| `internal/application/notification` | 活跃 | `app`、HTTP、native bridge | 既是 Web debug API，也是 Runtime 事件策略层 |
| `internal/application/operation` | 活跃 | `app`、SMS/eSIM/network/VoWiFi | `Cancel`、`Events` 没有当前 Web 调用者 |
| `internal/application/rawat` | 活跃 | `app`、HTTP | 仅在 backend 暴露 `raw_at` capability 时成功 |
| `internal/application/sms` | 活跃 | `app`、HTTP、notification baseline | SQLite 外发短信持久化已接入 |
| `internal/application/vowifi` | 部分活跃 | `app`、HTTP、Runtime 事件 | Service/Host 已启动，但当前默认 AT/Command backend 没有 VoWiFi port |
| `internal/backend` | 部分活跃 | Runtime、application、平台 Darwin | AT/Command 当前生产活跃；QMI/MBIM 文件未被根入口装配 |
| `internal/apduarbiter` | 间接活跃 | `internal/modem`、`internal/esim` | 由 APDU 会话和 eSIM 切换路径使用，没有独立 Web 入口 |
| `internal/config` | 部分活跃 | `backend/at_factory`、`modem.Manager` | `DeviceConfig` 活跃；全局配置管理和 YAML 持久化无当前生产调用 |
| `internal/domain/device` | 活跃 | Runtime、backend、platform、HTTP | 状态机、Capability、Candidate、Snapshot 是共享领域契约 |
| `internal/domain/errors` | 活跃 | Runtime、application、backend、platform、HTTP | API 错误序列化依赖其稳定 code |
| `internal/esim` | 部分活跃 | `platform/darwin/esim_port` | Darwin USB eSIM 路径活跃；QMI/MBIM transport 只在未装配路径或测试中使用 |
| `internal/esim/pki` | 间接活跃 | `internal/esim` | 通过 `embed` 提供 eSIM PKI 数据，不直接被 app 导入 |
| `internal/modem` | 活跃 | `backend/at_factory`、`backend/at_backend`、Darwin discovery | 大量旧式 Manager API 不是当前 HTTP 直接入口，但 AT 状态/SMS/APDU 依赖它 |
| `internal/platform/darwin` | 活跃（macOS） | `app.New` | 当前主要硬件发现、USB AT、网络和 eSIM 平台实现 |
| `internal/platform/darwin/native` | 条件活跃 | `app.newApp`、`cmd/djonehub` | macOS cgo 使用真实 Bridge；非 Darwin 使用 stub |
| `internal/platform/linux` | 条件活跃 | `app.New` on Linux | sysfs/network 代码可进入；仅 control path 的 modem 尚未接入当前 ATFactory |
| `internal/platform/windows` | 占位活跃 | `app.New` on Windows | 仅包裹 unsupported adapter，硬件验证尚未实现 |
| `internal/platform/unsupported` | 活跃 | offline、Windows、Linux/Darwin 嵌入 | 负责统一 capability-not-supported 行为和平台路径描述 |
| `internal/runtime` | 活跃 | `app`、HTTP、所有 application Service | 单设备状态机、Backend 生命周期、锁和 EventBus 核心 |
| `internal/simaid` | 间接/条件活跃 | `modem/commands.go` | 通过 SIM auth AID 解析使用；当前 VoWiFi capability 未闭合，Web 通常到不了此路径 |
| `internal/storage` | 活跃 | `app`、SMS、Extras | SQLite 是当前主存储；JSONStore 仅用于 legacy migration 和测试 |
| `internal/testfixtures` | 测试专用 | backend/esim/modem 测试 | 无生产导入，可从发行构建依赖图中排除 |
| `internal/transport` | 活跃契约 | Runtime、application/network、platform | 只定义 discovery/network/tunnel 等平台边界，具体实现由 platform 包提供 |
| `internal/vowifihost` | 部分活跃 | `application/vowifi` | Host 状态机和恢复逻辑已启动，但缺少当前后端 VoWiFi port |

`internal/api`、`internal/application`、`internal/domain`、`internal/platform` 是目录命名空间，本身没有独立 Go 实现；实际使用应按其下的子 package 判断。

### 8.2 当前 Internal 生产主链

```text
cmd/djonehub
  -> internal/app
      -> internal/runtime
          -> internal/platform/{darwin|linux|windows}
          -> internal/backend
              -> internal/modem（串口 AT）
              -> internal/platform/darwin USB AT CommandBackend
      -> internal/application/*
          -> internal/api/http
          -> internal/storage SQLite
          -> internal/platform/darwin/native
```

主要跨模块依赖如下：

1. `app.newApp` 在 `internal/app/app.go:91-176` 创建 `SQLiteStore`，迁移 profile notes、通知偏好和短信历史，再把 namespace 或根 store 注入 `extras.Service`、`sms.Service` 和通知回调。
2. `runtime.Runtime` 在 `internal/runtime/runtime.go:43-177` 调用 `transport.DeviceDiscovery` 发现设备，通过 `backend.BackendFactory` 打开后端，并把 `backend.BackendEvent` 转为统一 EventBus 事件。
3. `application` 层只依赖 `backend`、`domain`、`runtime` 和 `transport` 契约，不直接操作 AT/QMI/MBIM 协议；这使 Web handler 与具体硬件协议隔离。
4. `platform/darwin/esim_port.go` 把 `internal/esim.Manager` 转成 `backend.ESIMPort`，所以 eSIM 复杂逻辑不是 HTTP 直连，而是经过平台适配器进入。
5. `platform/darwin/native` 实现 `notification.Sink` 并接收 Swift command；它与 HTTP 通知接口共享偏好和事件模型，但不是 Web API 的替代实现。

### 8.3 Internal 中明确的未接入候选

| 代码范围 | 当前证据 | 判定 |
|---|---|---|
| `internal/config/manager.go`、部分 `persist*.go` | 非测试生产代码没有调用 `InitGlobalManager`、`ReloadFromFile`、`GetConfig`、设备 YAML CRUD | 当前根入口未接入；可能是旧配置系统 |
| `internal/config` 中 Telegram/Feishu/QQ/Webhook/Proxy 等配置模型 | 当前 `app` 不加载 `config.Load`，也没有对应 application Service | 当前生产未接入 |
| `internal/esim/qmi_channel.go`、`qmi_uim_transport.go` | 仅由 QMI eSIM transport 或测试链引用 | 当前根入口未接入，属于 QMI 预留 |
| `internal/esim/mbim_apdu_transport.go` | 由 MBIM transport/测试引用，当前 `app.New` 不构造 MBIM backend | 当前根入口未接入 |
| `internal/backend/qmi_backend*.go`、`mbim_backend*.go` | `NewBackend` 未被根目录生产代码调用 | 当前根入口未接入，但同包编译和测试仍使用 |
| `internal/simaid` 的 Web 可达性 | 依赖 SIM auth/VoWiFi 等能力；当前默认后端无 VoWiFi port | 包有协议用途，但当前 Web 不可达 |
| `internal/platform/windows` 的真实硬件实现 | 只有 unsupported 包装 | Windows 硬件链未实现 |
| `internal/platform/linux` 的 QMI/MBIM control path | 发现可记录 `ControlPath`，但 `New()` 使用 `NewATFactory(nil)` | Linux 控制面链未闭合 |
| `internal/testfixtures` | 仅 `_test.go` 导入 | 测试专用，不进入生产运行 |
| `internal/storage.JSONStore` | 当前仅用于 SQLite migration 与独立测试 | 旧存储兼容层，不是新的运行时主存储 |

### 8.4 不应误删的“非 Web 直达”模块

以下模块没有自己的 HTTP handler，但仍被当前生产链间接使用：

- `internal/domain/device` 和 `internal/domain/errors`：所有 application/runtime/backend 的共享契约和错误边界。
- `internal/transport`：网络、发现、隧道平台接口；Web 业务通过 application 间接使用。
- `internal/runtime`：状态轮询、EventBus、资源锁和后端生命周期，WebSocket 与所有异步操作依赖它。
- `internal/modem`：ATBackend 的身份、注册、信号、SMS、Raw AT、APDU 和重启实现依赖它。
- `internal/apduarbiter`：被 modem 和 eSIM 管理器使用，用于 APDU 会话/切换期间的并发控制；当前 eSIM/AT path 的底层安全约束不能仅因没有独立 endpoint 删除。
- `internal/esim/pki`：通过 `internal/esim` 间接加载 eSIM PKI 数据。
- `internal/platform/unsupported`：offline 和其他平台的明确能力错误来源。


## 9. 当前生产未接入的后端代码

### 9.1 QMI/MBIM

根目录生产代码中可以找到这些构造函数定义，但没有非测试调用：

- `internal/backend/NewBackend`：`internal/backend/factory.go:47-75`
- `NewQMIBackend`：`internal/backend/qmi_backend.go:160`
- `NewMBIMBackend`：`internal/backend/mbim_backend.go:27`

当前生产装配事实是 `app.New` 使用 `backend.NewATFactory(...)`，而不是 `NewBackend`。因此以下代码当前不会被 Web 请求执行：

- `internal/backend/qmi_backend*.go`
- `internal/backend/mbim_backend*.go`
- `internal/backend/qmi_operator_selection.go`
- QMI/MBIM 专用 USSD、SIM auth、短信、信号和注册辅助逻辑
- 仅被上述实现使用的 `internal/esim/qmi_channel.go` 等 QMI 通道代码

它们仍然有两种现实用途：同一 Go package 编译需要它们通过类型检查；现有测试直接覆盖大量行为。若目标是裁剪当前 macOS 发行版，需要先确认未来是否还要保留 QMI/MBIM，再删除或移动到可选 module，不能仅凭“当前 Web 未调用”直接删除。

### 9.2 旧接口与适配层

`internal/backend/contracts.go`、`service_ports.go`、`business_adapter.go` 是当前 Runtime 和 application 使用的统一边界。`BusinessAdapter` 在 `internal/backend/business_adapter.go:21-60` 根据 legacy backend 的接口能力生成 capability set，并在 `:65-398` 做类型转换。

其中：

- `Identity/Radio/SIM/ListSMS/SendSMS/RawAT/Reboot` 在当前 AT/Command Web 链中有效。
- `ESIMPort` 在 macOS CommandBackend 路径有效，在 ATBackend 串口路径没有实现。
- `NetworkPort` 在 CommandBackend 路径有效；串口 AT 路径主要由平台 NetworkController 提供网络能力。
- `VoWiFiPort/VoWiFiServicePort` 当前默认后端没有实现，因此是已定义但当前硬件链未闭合的扩展接口。
- `USSD`、通用 `APDU`、SIM auth 辅助方法没有当前 Web API 入口；其中部分会被 eSIM/VoWiFi 的底层准备流程间接使用，不能按“没有 endpoint”简单删除。

## 10. 明确的未使用候选清单

以下结论仅表示“从当前根目录 Web 到生产入口的静态调用链没有到达”：

| 候选 | 证据 | 建议 |
|---|---|---|
| `api.capabilities` | 仅声明，无 Vue 调用 | 若不支持外部复用，可从前端 SDK 清理；后端接口先保留 |
| `api.sms` | 仅声明，无 Vue 调用 | 页面已用 refresh，可评估合并接口，但不要删除公共 GET |
| `api.operation` | 仅声明，无 Vue 调用 | 当前 Web 依赖 WS；若确认所有客户端都改用 WS，再评估接口 |
| `operation.Manager.Cancel` | 无当前 HTTP 路由/生产调用 | 若不计划取消异步任务，可移入内部实现或保留给未来 API |
| `operation.Manager.Events` | 当前生产调用未发现 | 可做 API 精简候选 |
| `sms.Service.Read` | 无 handler/生产调用 | 如果没有单条短信详情需求，可删除；先检查外部包调用约束 |
| `extras.Service.MarshalNotes` | 当前生产调用未发现 | 可删除或改为明确的导出/持久化 API |
| QMI/MBIM 实现 | 生产 factory 未装配，测试大量使用 | 先做产品平台决策，再裁剪；不建议把它们标为普通死代码 |

## 11. 复核结论与风险

1. “后端未被当前 Web 使用”和“接口没有价值”是两回事。OpenAPI、诊断、操作查询等接口可以供脚本、未来 Web 或测试使用。
2. Web 的操作状态主要来自 WebSocket，而不是 `GET /operations/{id}`；如果 WebSocket 丢事件，当前前端会重连并刷新设备状态，但不会自动用 `api.operation` 查询单个操作。这是现有设计取舍，也是操作查询 SDK 未使用的原因。
3. VoWiFi 路由和 UI 已存在，但当前默认 AT 后端 capability 没有闭合。需要硬件能力时，应先补 `VoWiFiPort` 适配或明确在前端隐藏，而不是只保留按钮。
4. Linux 的网络发现已经存在，但 `ATFactory(nil)` 没有接上仅有 control path 的 QMI/MBIM 设备，说明跨平台源码存在不等于当前生产链可运行。
5. 报告只做了静态调用链分析，不能证明外部 HTTP 客户端、脚本、反射、插件或发行包中的旧页面不会调用“当前 Web 未调用”接口。

## 12. 验证命令

用于复核本报告的主要命令：

```sh
rg -n 'HandleFunc|/api/v1/' internal/api/http web/src
rg -n 'api\\.[A-Za-z0-9_]+' web/src
rg -n 'NewQMIBackend|NewMBIMBackend|NewBackend|NewATFactory' internal cmd
go test -mod=mod ./...
```

当前工作区的验证前置条件尚未满足：`internal/storage/sqlite.go` 引入了 `modernc.org/sqlite`，但根目录 `go.mod` 尚未声明该依赖，因此 `go list`/`go test` 会先报 `no required module provides package modernc.org/sqlite`。此外，现有工作区改动已经移除了 `backend.SMSMessage.Code`，而 `internal/backend/business_adapter_test.go` 仍引用该字段；依赖补齐后还需要单独处理这个测试契约不一致。
