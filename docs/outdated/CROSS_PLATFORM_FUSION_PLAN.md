# DJOneHub 选择性融合与跨平台重构方案

## 1. 方案定位

本方案不是把 DJOneHub 和 `vohive-open` 合并成一个功能更大的 VoHive，也不是完整移植 VoHive 的所有页面和服务。

DJOneHub 仍然是产品主体，目标是改善当前工程的组织方式，并选择性借鉴 VoHive 中已经验证过的设计：

- 前端使用 Vue 3，而不是继续扩展当前内嵌原生 HTML 页面。
- 后端按应用服务、设备运行时、协议后端和平台适配器分层。
- AT、QMI、MBIM 是可替换的并列后端。
- VoWiFi/IMS 作为设备的一项可选能力接入统一生命周期。
- 使用统一 REST API 和 WebSocket 事件 API。
- 将 macOS、Linux、Windows 的差异限制在平台适配层。
- 首期只管理单个设备，不引入设备池和多设备调度模型。

核心目标是：在保留 DJOneHub 当前产品体验的前提下，让代码可以继续增加 QMI、MBIM、VoWiFi 和其他平台支持，而不再把所有逻辑堆在一个 macOS `main.go` 中。

## 2. 功能范围

### 2.1 首期必须支持

| 功能 | 说明 |
| --- | --- |
| 单设备发现与重连 | 管理当前连接的一个模块，处理拔出、重新枚举和端口变化 |
| 设备状态 | IMEI、ICCID、IMSI、SIM、注册、信号、运营商、网络制式 |
| AT 调试 | 在设备支持 `raw_at` 时提供原始 AT 命令 |
| 短信 | 读取、发送、长短信重组、验证码展示和必要的清理操作 |
| SIM/eSIM | APDU、EID、Profile 查询、下载、启用、改名和删除 |
| 网络诊断 | USB 网络模式、网卡、地址、默认路由、流量和连通性 |
| AT/QMI/MBIM 后端 | 根据设备配置和探测结果选择后端，不能把 AT 写死在业务层 |
| Vue 管理页面 | 面向 DJOneHub 当前功能设计，不复制完整 VoHive 管理后台 |
| REST/WebSocket API | REST 执行查询和命令，WebSocket 推送状态与长操作进度 |
| 跨平台构建 | Linux、macOS、Windows 先达到各自已验证的设备能力 |

### 2.2 选择性支持

| 功能 | 处理方式 |
| --- | --- |
| VoWiFi/IMS | 保留 VoHive 的运行时思路和 `vowifi-go` 接入；Linux 优先完成数据面，其他平台按能力逐步实现 |
| QMI 数据连接 | 在有合适控制设备和网卡实现的平台启用 |
| MBIM 数据连接 | 作为独立后端启用，不要求必须存在 AT 端口 |
| 本地配置和历史 | 只保存 DJOneHub 所需配置、Profile 备注和必要的操作记录 |
| 认证 | 首期以本机管理为主，保留统一 API 的鉴权边界，不引入完整的远程多租户系统 |

### 2.3 明确不纳入本次融合

以下功能不应为了“完整复用 VoHive”而加入 DJOneHub：

- 设备池、多设备并发管理和设备租约调度。
- 代理池、SOCKS5/HTTP 代理实例和上游代理编排。
- Telegram、Email、Bark、飞书、QQ 等通知渠道。
- QQ Bot、接码中心、远程代理运营和国家路由规则。
- VoHive 的完整后台页面、代理页面和与 DJOneHub 无关的设置页面。
- 为了未来多设备而提前引入复杂的设备聚合、分布式任务或多租户数据库模型。
- 未经平台验证就承诺 macOS 或 Windows 的完整 VoWiFi 数据面。

这些功能以后可以作为独立项目或可选模块讨论，但不属于当前 DJOneHub 融合目标。

## 3. 当前工程判断

### 3.1 DJOneHub 当前基础

根工程已经包含适合继续演进的代码：

- `cmd/djonehub-macos/main.go`：当前服务入口、HTTP 路由、设备状态、短信、eSIM 和网络逻辑。
- `internal/backend`：AT、QMI、MBIM 后端接口和部分实现。
- `internal/modem`：AT 设备管理、状态查询和命令执行。
- `internal/esim`、`internal/apduarbiter`：eSIM、APDU 和并发协调。
- `pkg/mbim`：MBIM 协议实现和测试。
- `pkg/smscodec`：SMS PDU 编解码和长短信重组。
- `web/`：DJOneHub 统一 Vue 管理页面。

当前主要问题不是缺少所有功能，而是入口层承担了过多职责：HTTP handler、设备生命周期、AT 命令、短信、eSIM、网络命令和平台判断混在一起。

### 3.2 VoHive 中值得借鉴的部分

只借鉴以下设计和实现，不整体搬入 VoHive 产品：

- `internal/backend` 中 AT、QMI、MBIM 的统一能力接口。
- `internal/vowifihost` 中按设备串行化 VoWiFi 启停、恢复和切换的思路。
- `third_party/vowifi-go` 中 SIM/ISIM AKA、SWu/ePDG、IMS 和数据面运行时。
- `internal/api` 中 REST 分组、统一错误、OpenAPI 和异步操作处理方式。
- `web` 中 Vue 3、TypeScript、Pinia、Vite 的工程实践，但不直接把完整 VoHive Web 作为 DJOneHub 页面。
- VoHive 对 QMI/MBIM 事件、设备状态和操作进度的事件化处理。

## 4. 目标架构

```text
DJOneHub Vue 3 管理端
        │ REST / WebSocket
API 适配层：鉴权、DTO、错误、事件订阅
        │
应用服务：设备状态、短信、eSIM、网络、AT、VoWiFi
        │
单设备运行时：发现、连接、后端选择、重连、资源锁
        │
AT Backend │ QMI Backend │ MBIM Backend
        │
Serial / USB / QMI / MBIM / APDU / Network / Packet Tunnel
        │
Linux Adapter │ macOS Adapter │ Windows Adapter
```

### 4.1 依赖方向

```text
cmd
  -> app/bootstrap
     -> api/http, api/ws
        -> application/usecase
           -> domain + ports
              -> backend / transport / platform
```

必须遵守：

- `domain` 不依赖 Gin、数据库驱动、操作系统 API、设备路径或 `os/exec`。
- `application` 不直接打开串口、执行 AT、读 `/dev` 或修改系统路由。
- API handler 只负责鉴权、参数校验、DTO 转换和调用 application service。
- AT/QMI/MBIM 后端只实现协议和设备能力，不负责 HTTP 或页面状态。
- Vue 页面根据服务端 capability 判断功能，不根据 `darwin`、`linux`、`windows` 写业务分支。
- VoWiFi 运行时由应用服务和设备运行时管理，API handler 不直接创建或销毁底层 session。

### 4.2 建议目录

```text
.
├── cmd/
│   └── djonehub/
├── internal/
│   ├── app/                    # 启动、关闭、依赖注入
│   ├── domain/
│   │   ├── device/             # 单设备状态、能力和生命周期
│   │   ├── sms/
│   │   ├── sim/
│   │   ├── esim/
│   │   ├── network/
│   │   └── vowifi/
│   ├── application/
│   │   ├── device/
│   │   ├── sms/
│   │   ├── esim/
│   │   ├── network/
│   │   └── vowifi/
│   ├── api/
│   │   ├── http/               # REST v1、鉴权、DTO、OpenAPI
│   │   └── ws/                 # WebSocket 会话和事件广播
│   ├── runtime/                # 单设备 worker、重连、资源锁
│   ├── backend/                # AT、QMI、MBIM
│   ├── transport/              # Serial、USB、QMI、MBIM、APDU
│   ├── storage/                # 配置、必要的本地数据和迁移
│   ├── vowifihost/             # VoWiFi 生命周期适配
│   └── platform/
│       ├── linux/
│       ├── darwin/
│       └── windows/
├── pkg/
│   ├── mbim/
│   ├── smscodec/
│   └── apdu/
├── web/                        # DJOneHub 自己的 Vue 3 应用
├── packaging/
└── docs/
```

不设置 `devicepool`、`devices` 多设备聚合或设备租约层。单设备运行时先把资源生命周期和重连处理清楚，未来如果确实需要多设备，再单独评估是否抽象设备池。

## 5. 单设备运行时

### 5.1 生命周期

单设备运行时管理一台物理设备：

```text
absent -> discovered -> connecting -> initializing -> ready
                                              │
                                  degraded <- operation failed
                                              │
                              disconnected <- device removed
```

需要处理：

- DJI/Quectel USB 身份识别和物理位置关联。
- AT 串口、QMI 控制设备、MBIM 控制设备和网卡的端口映射。
- USB 模式切换引起的设备重新枚举。
- 设备拔出、超时、模组重启和后端重新初始化。
- AT、QMI、MBIM、APDU 操作之间的资源锁和取消。
- 当前设备的 capability 快照和状态事件。

设备 ID 可以使用物理位置、VID/PID、IMEI、序列号等稳定信息组合，但不需要设计多设备全局索引或设备池 API。

### 5.2 后端接口

AT、QMI、MBIM 必须对上层提供同一组业务能力，具体支持范围由 capability 表示：

```go
type ModemBackend interface {
    Mode() BackendMode
    Identity(context.Context) (Identity, error)
    Radio(context.Context) (RadioState, error)
    SIM(context.Context) (SIMState, error)
    SMS(context.Context) (SMSService, error)
    USSD(context.Context) (USSDService, error)
    APDU(context.Context, APDURequest) (APDUResponse, error)
    Capabilities(context.Context) CapabilitySet
    Close() error
}
```

实现边界：

- AT：AT 命令、短信、USSD、SIM 认证、厂商扩展和原始调试。
- QMI：NAS、WMS、UIM、DMS、数据连接、事件订阅和需要的 IMS 状态。
- MBIM：基础连接、短信、UICC、注册、信号、数据服务和指示消息。
- eSIM：只依赖 APDU/SIM 端口，不依赖 `ATBackend` 具体类型。
- VoWiFi：通过 SIM/ISIM AKA、设备身份、网络和数据面端口工作。

后端选择由设备配置、接口探测和能力协商完成。不能因为当前 DJOneHub 使用 AT，就让短信、eSIM 或 VoWiFi 的业务服务直接依赖 AT manager。

## 6. VoWiFi/IMS

VoWiFi 是本次选择性融合中明确保留的 VoHive 能力，但只实现与 DJOneHub 设备管理相关的部分。

### 6.1 运行时职责

`internal/vowifihost` 或等价的 VoWiFi service 负责：

- 启用、禁用、重连、恢复和设备模式切换。
- 等待 AT/QMI/MBIM 后端和 SIM/APDU 通道就绪。
- 获取 SIM/ISIM AKA 所需数据。
- 管理 ePDG、SWu/IKE、IMS 注册和数据面状态。
- 处理网络变化、模组重置、SIM 切换和失败恢复。
- 发布 VoWiFi、IMS、隧道和恢复进度事件。

不在本次范围内的 VoWiFi 功能：完整的通话中心、多用户 VoWiFi 管理、跨设备调度和运营级统计。

### 6.2 平台策略

当前 `vowifi-go` 的 TUN/XFRM 数据面存在 Linux 专用实现，平台能力必须如实表达：

- Linux：优先完成现有 VoWiFi/IMS 数据面和真实硬件验证。
- macOS：先完成 capability、状态展示、控制面和失败提示；数据面需要单独验证 Network Extension 或用户态隧道。
- Windows：先完成设备后端和网络基础能力；数据面需要单独验证 Wintun 或 Windows 网络方案。

例如：

```json
{
  "vowifi": {
    "available": false,
    "reason": "packet_tunnel_not_supported",
    "operations": ["inspect"]
  }
}
```

## 7. REST 与 WebSocket API

### 7.1 API 范围

使用 `/api/v1` 版本前缀，面向单设备设计：

```text
GET  /api/v1/device
GET  /api/v1/device/capabilities
GET  /api/v1/device/status
POST /api/v1/device/actions/rescan
POST /api/v1/device/actions/at
GET  /api/v1/sms
POST /api/v1/sms/send
GET  /api/v1/esim
POST /api/v1/esim/actions/download
POST /api/v1/esim/actions/switch
GET  /api/v1/network
PATCH /api/v1/network/mode
PATCH /api/v1/vowifi
POST /api/v1/vowifi/actions/reconnect
WS   /api/v1/events/ws
```

REST 用于：

- 获取单设备状态和 capability。
- 提交短信、eSIM、AT、网络模式和 VoWiFi 操作。
- 获取长操作的 `operation_id` 和最终结果。
- 提供 OpenAPI 文档和统一错误。

不为代理、通知、多设备和 VoHive 无关功能预留完整 API 资源。

### 7.2 WebSocket 事件

统一事件封套：

```json
{
  "id": "evt_01J...",
  "type": "device.status.changed",
  "version": 1,
  "occurred_at": "2026-08-01T12:00:00Z",
  "data": { "state": "ready", "backend": "qmi" }
}
```

至少支持：

- 设备发现、连接、断开和恢复。
- 信号、注册、SIM、网络和流量变化。
- 新短信和短信发送结果。
- eSIM 下载、切换和删除进度。
- AT/QMI/MBIM 后端事件和错误。
- VoWiFi/IMS、ePDG、隧道和恢复状态。

连接建立后先发送当前单设备快照，再发送增量事件。事件广播由运行时统一完成，不能由每个 handler 自己维护轮询和推送逻辑。

统一错误格式：

```json
{
  "error": {
    "code": "capability_not_supported",
    "message": "当前设备没有 MBIM 数据连接能力",
    "retryable": false,
    "details": { "capability": "data.mbim" }
  }
}
```

## 8. Vue 前端方案

### 8.1 前端定位

新建 DJOneHub 自己的 `web/` Vue 应用，页面只覆盖本项目范围内的功能：设备状态、短信、eSIM、网络、AT 和 VoWiFi。

`vohive-open/web` 可以作为以下内容的参考或来源：

- Vue 3 + Vite + TypeScript 的构建配置。
- Pinia store、HTTP service、事件订阅和类型组织方式。
- 可复用的状态灯、设备详情、进度和错误展示组件。

但不直接把 `vohive-open/web` 全部作为 DJOneHub 的管理后台，也不迁入代理、通知、机器人等页面。前端信息架构应以 DJOneHub 的单设备工作流为中心。

### 8.2 页面

- 首页/设备状态：模块连接状态、SIM、信号、注册、运营商、后端和工作模式。
- 短信：收件箱、发送、长短信、验证码和模块存储清理。
- eSIM：EID、Profile、下载、启用、改名、删除和操作进度。
- 网络：USB 网络模式、网卡、默认路由、流量、模块连通性和诊断。
- AT 调试：仅在有 `raw_at` capability 时显示。
- VoWiFi：启用、注册、隧道、IMS、恢复和错误状态。
- 设置：设备身份偏好、轮询、日志、数据目录和本地安全设置。

页面规则：

- 不根据操作系统名称决定按钮，而是根据 capability 决定显示和可用性。
- 所有长操作使用 `operation_id` 和 WebSocket 事件显示进度。
- 不支持的能力显示明确原因，不静默隐藏为异常空白。
- 页面 API 类型和后端 DTO 一起维护，避免手工散落 JSON 字段。

## 9. 平台适配

| 能力 | Linux | macOS | Windows | 首期策略 |
| --- | --- | --- | --- | --- |
| Vue 管理端和服务端 | 支持 | 支持 | 支持 | 同一 API 和前端代码 |
| AT 串口 | tty | libusb/IOKit/串口 | COM/WinUSB | 首期重点 |
| 设备发现 | udev | IOKit/libusb | SetupAPI/WinUSB/COM | 单设备热插拔 |
| QMI | 优先 | 按硬件验证 | 按硬件验证 | 后端可选 |
| MBIM | 支持 | 按硬件验证 | 原生 MBIM 或适配 | 后端可选 |
| SMS/USSD/SIM | 按 capability | 按 capability | 按 capability | 保持业务一致 |
| eSIM/APDU | 支持 | 支持 | 按硬件验证 | 保留现有能力 |
| 网络接口和路由 | netlink | 系统网络 API | Windows 网络 API | 平台适配器实现 |
| VoWiFi 数据面 | 优先 | 单独验证 | 单独验证 | 不虚假承诺 |
| 安装服务 | systemd/Docker | launchd/安装包 | Windows Service/MSI | 后续完善 |

共享代码不得直接调用 `ifconfig`、`networksetup`、`ip`、PowerShell 或硬编码 `/dev/ttyUSB*`。这些操作必须通过 `NetworkController`、`SerialTransport`、`DeviceDiscovery` 等端口完成。

## 10. 实施阶段

### F0：确定范围和契约

- 固定当前 DJOneHub 和 `vohive-open` 的来源、commit 和许可证记录。
- 明确只融合 VoWiFi、AT/QMI/MBIM、API 和跨平台适配相关内容。
- 明确排除设备池、代理、通知、机器人和完整 VoHive Web。
- 定义单设备领域模型、capability、错误码、REST v1 和 WebSocket 事件。
- 列出当前 `main.go` 中每个函数属于 domain、application、backend、transport 还是 macOS adapter。

验收：范围外功能不会出现在目标目录、API 和验收清单中。

### F1：建立 Vue 页面和 API 骨架

- 在根工程建立 `web/` Vue 3 + Vite + TypeScript 项目。
- 参考 `vohive-open/web` 的工程实践，但只实现 DJOneHub 页面。
- 将当前原生页面的功能拆成 Vue view、component、service、store 和类型。
- 建立 `/api/v1` REST、鉴权、错误格式和 OpenAPI 基础。
- 建立单设备 WebSocket 快照和事件广播。
- 保留当前 API 的兼容路由，待 Vue 页面切换完成后再删除。

验收：无硬件 demo 可以打开 Vue 页面，展示单设备离线状态，并能验证 API/WS 契约。

### F2：拆分后端代码

- 从 `cmd/djonehub-macos/main.go` 抽取设备状态、短信、eSIM、网络和 AT 调试用例。
- 建立 `application` 服务和 `ports` 接口。
- 将现有 `internal/modem`、`internal/esim`、`pkg/mbim`、`pkg/smscodec` 接入新边界。
- 把 macOS USB 身份、端口枚举、系统网络和进程操作迁到 `platform/darwin`。
- 为每个用例增加内存 backend、假传输和故障注入测试。

验收：应用服务可以在没有 macOS 命令和真实设备节点的情况下运行测试。

### F3：统一 AT/QMI/MBIM

- 以统一 `ModemBackend` 为业务入口，补齐三种后端的能力映射。
- 保证 MBIM/QMI 模式不依赖 AT 端口才能启动。
- 统一后端生命周期、事件订阅、超时、关闭和重新初始化。
- 将 eSIM/APDU、短信、网络和 VoWiFi 需要的端口能力明确化。
- 为每个后端建立协议测试、模拟响应和 capability 测试。

验收：同一个单设备运行时可以根据配置和探测结果选择 AT、QMI 或 MBIM；不支持的操作返回标准错误。

### F4：接入 VoWiFi

- 将 `vowifihost` 接入单设备运行时，而不是在 API 中直接操作 `vowifi-go`。
- 复用 VoHive 的生命周期、恢复和 SIM/ISIM AKA 适配思路。
- Linux 完成 VoWiFi/IMS 数据面和真实硬件验证。
- macOS、Windows 先提供状态、能力和明确错误，再分别验证数据面方案。
- 将 VoWiFi 状态、隧道和恢复事件接入 WebSocket。

验收：VoWiFi 与设备拔出、SIM 切换、网络切换和模组重启的行为可预测、可恢复。

### F5：平台和发行

- Linux：udev、QMI/MBIM、netlink、TUN/XFRM、systemd/Docker。
- macOS：DJI/Quectel USB、libusb/IOKit、串口、系统网络、launchd 和安装包。
- Windows：SetupAPI、COM、WinUSB、MBIM、Windows 网络 API 和服务安装。
- 为每个平台建立日志目录、配置目录、权限、升级和卸载策略。
- 建立构建、签名、校验和、SBOM 及真实硬件回归流程。

验收：每个平台都能构建、启动并报告真实能力；能力未完成时不会显示为已支持。

## 11. 测试策略

### 11.1 单元和契约测试

- AT 响应、SMS PDU、长短信和验证码解析。
- MBIM/QMI 消息、事件、超时和断线。
- APDU、SIM 认证和 eSIM Profile 状态。
- 单设备生命周期、端口映射、重连和资源锁。
- capability 计算、统一错误和 REST/ WebSocket 契约。
- VoWiFi 启停、恢复、过期命令和平台不支持错误。
- Vue 类型检查、API service 和 WebSocket 重连。

### 11.2 无硬件测试

至少覆盖：

- 设备离线、发现、连接、拔出和重新枚举。
- AT、QMI、MBIM 三种后端的最小成功与失败路径。
- 短信、eSIM、网络、AT 和 VoWiFi 的 capability 展示。
- REST 命令、异步操作、WebSocket 快照和增量事件。
- 后端初始化失败、端口冲突、网络不可用和模组重置。

### 11.3 真实硬件测试

记录操作系统、CPU 架构、模块型号、固件、VID/PID、USB 模式、后端类型、SIM/eUICC、运营商和失败日志。至少覆盖：

- macOS：DJI/Quectel 设备发现、AT、短信、eSIM、USB 网络和网络诊断。
- Linux：AT、QMI、MBIM、短信、eSIM、网络和 VoWiFi。
- Windows：设备发现、COM/MBIM、基础短信和网络能力；未验证的 eSIM/VoWiFi 不宣称完成。

## 12. 不接受的实现方式

- 为了“融合完整”而迁入 VoHive 的全部代理、通知、机器人和后台页面。
- 把 `vohive-open/web` 直接作为完整 DJOneHub 管理后台，导致页面范围反向膨胀。
- 引入设备池、多设备调度或租约系统解决当前不存在的问题。
- 保留所有业务在 `cmd/djonehub-macos/main.go` 中，只增加更多 `if runtime.GOOS` 分支。
- 让 QMI/MBIM/VoWiFi 只存在于文档或页面按钮中，没有后端接口和测试。
- 在共享代码中直接调用平台命令或假设固定设备路径。
- 以交叉编译成功代替真实硬件验证。

## 13. 完成定义

本次重构完成时应满足：

- DJOneHub 仍是单设备本地管理产品，范围没有扩展成完整 VoHive 平台。
- 管理页面是 DJOneHub 自己的 Vue 应用，不再继续扩展原生 HTML 页面，也不强制采用完整 `vohive-open/web`。
- 设备、短信、eSIM、网络、AT 和 VoWiFi 由应用服务组织，入口层只负责组装和协议适配。
- AT、QMI、MBIM 可替换，且每种后端都能报告实际 capability。
- REST v1 和 WebSocket 事件 API 可被 Vue 页面和 CLI 复用。
- Linux、macOS、Windows 的平台代码边界清晰，不支持能力有标准错误。
- Linux 完成 QMI/MBIM/VoWiFi 闭环；macOS 完成当前 DJOneHub 核心闭环；Windows 完成已声明的基础能力。
- 没有引入设备池、代理池、通知平台或与当前目标无关的 VoHive 功能。

## 14. 首批任务

1. 在本文件基础上冻结范围外清单和单设备 API 契约。
2. 创建 DJOneHub `web/` Vue 骨架，先迁移设备状态和网络页面。
3. 为 `main.go` 的设备状态用例建立 application service 和平台端口。
4. 将现有 AT、MBIM、SMS、eSIM 代码接入新的单设备运行时。
5. 增加 QMI/MBIM capability 和统一错误测试。
6. 建立 WebSocket 单设备快照、状态事件和长操作进度。
7. 在 Linux 验证 QMI/MBIM/VoWiFi，在 macOS 验证 DJI/Quectel 核心功能，再评估 Windows 扩展。

最终交付顺序是：

```text
冻结范围 -> Vue/API 骨架 -> 单设备后端分层 -> AT/QMI/MBIM
       -> VoWiFi -> Linux/macOS/Windows 平台验证
```
