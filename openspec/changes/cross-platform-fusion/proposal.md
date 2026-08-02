## Why

DJOneHub 当前将 HTTP handler、设备生命周期、AT 命令、短信、eSIM、网络操作和 macOS 平台逻辑集中在 `cmd/djonehub-macos/main.go`，使新增 QMI、MBIM、VoWiFi 及 Linux/Windows 支持的成本和回归风险持续增加。现在需要在保留 DJOneHub 单设备产品边界的前提下，建立可替换的后端、统一 API、事件化运行时和平台适配边界。

## What Changes

- 建立单设备领域模型和运行时，统一设备发现、初始化、拔出、重连、资源锁及 capability 快照。
- 将 AT、QMI、MBIM 作为可替换的并列 modem backend，由 capability 和探测结果决定可用操作。
- 将设备状态、短信、eSIM、网络、AT 调试和 VoWiFi 组织为 application service，通过端口访问传输、存储和平台能力。
- 提供面向单设备的 `/api/v1` REST API、统一错误、异步操作结果和 WebSocket 快照/增量事件。
- 新建 DJOneHub 自己的 Vue 3 + TypeScript + Vite 管理端，按服务端 capability 展示功能。
- 将 Linux、macOS、Windows 的设备发现、串口、网络、隧道和服务安装差异限制在平台适配层。
- 接入 VoWiFi/IMS 生命周期和恢复状态；Linux 优先完成数据面，macOS/Windows 对未验证的数据面如实报告不支持。
- **BREAKING**: 新增统一 `/api/v1` 契约，并在 Vue 迁移完成后逐步淘汰当前原生页面和入口层直连业务逻辑。
- 明确不引入设备池、多设备调度、代理池、通知渠道、机器人、远程多租户或完整 VoHive 管理后台。

## Capabilities

### New Capabilities

- `single-device-runtime`: 单设备发现、连接、后端选择、生命周期、重连、资源锁和 capability 快照。
- `modem-backends`: AT、QMI、MBIM 的统一业务能力接口、后端选择、事件、超时、关闭和不支持能力错误。
- `device-services`: 设备状态、短信、SIM/eSIM、网络、AT 调试和长操作的 application service 边界。
- `device-api`: 单设备 REST v1 API、DTO、统一错误、鉴权边界、OpenAPI 和异步操作契约。
- `device-events`: WebSocket 快照、增量事件、事件封套、操作进度和断线重连语义。
- `vowifi-lifecycle`: VoWiFi/IMS 启停、恢复、网络/SIM/设备变化处理和平台 capability 表达。
- `vue-management-ui`: 面向 DJOneHub 单设备工作流的 Vue 3 管理页面、状态 store、API service 和 capability 驱动界面。
- `platform-adapters`: Linux、macOS、Windows 的设备发现、传输、网络、隧道和服务运行适配边界。

### Modified Capabilities

无。当前 `openspec/specs/` 中没有可修改的既有能力规格。

## Impact

- Go 入口和内部包：`cmd/djonehub-macos/main.go`、`internal/backend`、`internal/modem`、`internal/esim`、`internal/apduarbiter`、`pkg/mbim`、`pkg/smscodec` 将接入新的应用、运行时、传输和平台边界。
- 新增 `cmd/djonehub`、`internal/app`、`internal/domain`、`internal/application`、`internal/api`、`internal/runtime`、`internal/transport`、`internal/platform`、`internal/storage`、`internal/vowifihost` 和 `web` 目录。
- 新增 REST/WebSocket API、OpenAPI 类型、事件协议和本地管理端；现有页面路由需要保持迁移期兼容。
- 依赖和验证范围扩展到 Vue/Vite/TypeScript、QMI/MBIM、平台设备 API、网络适配器及 `vowifi-go` 运行时。
- 测试需覆盖无硬件模拟、后端契约、生命周期/重连、API/事件以及 Linux/macOS/Windows 的已声明硬件能力。
