## Context

DJOneHub 当前的 macOS 入口同时承担启动、HTTP 路由、设备状态、AT、短信、eSIM、网络和平台命令。仓库已有 `internal/backend`、`internal/modem`、`internal/esim`、`internal/apduarbiter`、`pkg/mbim` 和 `pkg/smscodec`，但这些能力尚未通过统一的单设备运行时和应用服务组织起来。`vohive-open` 提供可参考的 backend、事件 API、Vue 和 VoWiFi 生命周期实现，但本变更只选择性吸收这些边界和实现经验。

本设计必须保持 DJOneHub 是单设备本地管理产品。领域层不能依赖 HTTP、Gin、数据库驱动、操作系统 API、设备路径或 `os/exec`；共享业务代码不能按 `GOOS` 分支执行系统命令。真实硬件能力可能因平台和模块不同而不同，所有未验证能力必须通过 capability 和标准错误表达。

## Goals / Non-Goals

**Goals:**

- 用单设备运行时管理发现、连接、初始化、就绪、降级、断开和重连。
- 让 AT、QMI、MBIM 通过统一能力端口服务上层，并且可以独立初始化和关闭。
- 将设备、短信、eSIM、网络、AT 调试和 VoWiFi 用例迁入 application service。
- 提供版本化 REST/WebSocket 契约，支持状态快照、增量事件和长操作进度。
- 建立 capability 驱动的 Vue 3 管理端和 Linux/macOS/Windows 平台适配层。
- 保持迁移期间当前 macOS 路由可用，并用无硬件测试验证新边界。

**Non-Goals:**

- 不实现设备池、多设备调度、租约、代理池、通知、机器人或远程多租户。
- 不迁移完整 `vohive-open/web`，不添加与 DJOneHub 无关的后台页面。
- 不把交叉编译当作平台完成证明；真实硬件验证仍按能力和平台单独记录。
- 不在 macOS 或 Windows 未验证数据面前宣称 VoWiFi 完整可用。

## Decisions

### 1. 采用 domain/application/ports/adapter 分层

`internal/domain` 保存单设备、短信、SIM/eSIM、网络和 VoWiFi 的状态及规则；`internal/application` 编排用例；`internal/transport`、`internal/backend`、`internal/platform`、`internal/storage` 实现端口；`internal/api` 只做鉴权、校验、DTO 和调用。这样可以在无设备、无 macOS 命令的测试中运行 application service。

备选方案是继续扩展 `cmd/djonehub-macos/main.go`，或直接复制 VoHive 的服务结构。前者无法隔离平台差异，后者会带入不属于 DJOneHub 的产品边界，因此不采用。

### 2. 以单设备运行时作为唯一设备生命周期拥有者

运行时内部维护一个设备状态机、当前设备句柄、后端选择结果、资源锁和事件总线。HTTP handler、WebSocket handler 和 VoWiFi service 都通过运行时或 application service操作，不直接打开传输或创建底层 session。设备拔出时由运行时取消未完成操作、关闭后端、发布断开事件并重新进入发现流程。

备选方案是让每个 API handler 自己轮询设备或维护 WebSocket 推送，这会产生重复状态源和竞态，因此不采用。

### 3. 统一业务能力而不是统一协议细节

`ModemBackend` 暴露身份、无线状态、SIM、SMS、USSD、APDU、能力查询、事件订阅和关闭等业务端口；AT/QMI/MBIM 实现各自协议转换。数据连接、厂商扩展和 VoWiFi 所需能力通过 capability 子集及专用端口表达，不强迫所有后端实现空洞的全功能接口。

后端选择顺序由设备配置、可探测接口和能力协商决定，并记录选择原因。QMI/MBIM 模式不能要求存在 AT 端口；eSIM 只依赖 APDU/SIM 端口。

### 4. 使用 REST 命令 + WebSocket 事件承载长操作

查询和命令统一挂在 `/api/v1`。短查询直接返回结果；短信发送、eSIM 下载/切换、VoWiFi 重连等长操作返回 `operation_id`，进度和最终结果通过事件总线广播。WebSocket 建立后先发送当前单设备快照，再发送带版本和时间戳的增量事件。

备选方案是让前端定时轮询每个资源，或为每个资源设计独立 SSE 流。统一 WebSocket 能复用运行时事件，减少连接和状态拼装，因此优先采用。

### 5. 前端按 capability 渲染，不按操作系统渲染

Vue 应用维护设备、能力、操作和事件 store，API 类型与 DTO 一起维护。页面在 capability 不存在时显示不可用原因和可执行的 inspect 操作；不能通过 `darwin`、`linux` 或 `windows` 分支决定业务按钮。迁移期保留旧路由作为兼容入口。

### 6. 平台差异用适配器和构建约束隔离

`DeviceDiscovery`、`SerialTransport`、`NetworkController`、`PacketTunnel` 和服务安装等端口由 `platform/linux`、`platform/darwin`、`platform/windows` 实现。共享层只依赖端口；平台实现负责 udev/IOKit/SetupAPI、netlink/系统网络 API、TUN/XFRM 等差异。能力注册在启动时生成，未实现的适配器返回结构化不支持错误。

### 7. 按 F0-F5 分阶段迁移，并保持可回退

先冻结范围和契约，再建立 Vue/API 骨架，之后迁移设备服务和后端，接入 VoWiFi，最后扩展平台发行。每一阶段都保留旧入口或兼容路由，使用 feature flag 或配置选择新运行时；若新运行时无法初始化，可回退到现有 macOS 服务，同时保留故障日志和 capability 快照。

## Risks / Trade-offs

- [Risk] 统一 backend 接口掩盖 AT、QMI、MBIM 的能力差异。→ 使用 capability 集合、专用端口和标准 `capability_not_supported` 错误，并为每个后端测试映射。
- [Risk] USB 模式切换会导致端口瞬时消失和设备 ID 改变。→ 用物理位置、VID/PID、IMEI/序列号等信息建立关联，运行时对重新枚举设置退避和取消语义。
- [Risk] WebSocket 事件丢失后前端状态过期。→ 连接建立发送快照，事件带 ID/版本；检测断序时重新请求快照。
- [Risk] VoWiFi 数据面在 macOS/Windows 不可移植。→ 将数据面能力单独建模，平台未验证时只提供 inspect/状态和明确错误。
- [Risk] 迁移期间新旧 API 产生行为差异。→ 先建立契约和适配测试，旧路由调用同一 application service，并在硬件回归后再移除旧页面。
- [Risk] 跨平台适配扩大首期工作量。→ 首期按平台能力矩阵交付，不以所有平台功能对齐为前提；Linux、macOS、Windows 分别记录已验证能力。

## Migration Plan

1. F0 固定来源、许可证、范围外清单、单设备模型、capability、错误、REST 和事件契约。
2. F1 创建 `web/` 和 `/api/v1` 骨架，实现离线页面、快照和事件广播，并保留兼容路由。
3. F2 从 macOS 入口抽取 application service、端口和平台适配，接入现有 AT/SMS/eSIM/MBIM 包并补无硬件测试。
4. F3 完善 AT/QMI/MBIM 选择、能力映射、事件、超时和重新初始化。
5. F4 将 VoWiFi 接入运行时，先完成 Linux 数据面与硬件验证，再对 macOS/Windows 提供真实能力报告。
6. F5 完成各平台设备发现、网络、发行、权限和硬件回归记录。

迁移期间新入口通过配置启用；出现初始化或回归问题时关闭新入口并继续使用兼容的 macOS 路由。回滚不得删除已有配置、短信或 eSIM 数据；稳定后再删除原生页面和旧入口中的重复业务代码。

## Open Questions

- 当前 macOS 入口的兼容路由和新 `/api/v1` 路由最终采用何种版本保留周期。
- 单设备稳定 ID 在不同 USB 模式和模块重启后的具体优先级及持久化格式。
- eSIM Profile 下载和删除的本地操作记录需要保存哪些字段及保留周期。
- macOS Network Extension 与 Windows Wintun/原生网络 API 的 VoWiFi 数据面方案和权限模型。
- Vue 工程是否复用 `vohive-open/web` 的依赖版本，还是按 DJOneHub 当前构建环境重新锁定版本。
