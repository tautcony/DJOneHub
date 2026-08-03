## 1. 范围与契约冻结

- [x] 1.1 记录 DJOneHub 与 `vohive-open` 的来源 commit、许可证和允许复用的目录，形成 F0 追踪文件
- [x] 1.2 将设备池、多设备调度、代理、通知、机器人、远程多租户和完整 VoHive Web 加入范围外检查清单
- [x] 1.3 定义单设备领域模型、生命周期状态、稳定设备 ID、后端模式和 capability 命名
- [x] 1.4 定义统一错误码、`capability_not_supported` 详情格式、retryable 规则和 HTTP 映射
- [x] 1.5 定义 `/api/v1` REST 路由、请求/响应 DTO、`operation_id` 和 OpenAPI 初版
- [x] 1.6 定义 WebSocket 事件封套、事件类型、快照、事件版本和断序重同步规则
- [x] 1.7 梳理 `cmd/djonehub-macos/main.go` 函数清单，标注 domain、application、backend、transport 和 macOS adapter 归属

## 2. 单设备运行时与端口

- [x] 2.1 创建 `cmd/djonehub`、`internal/app`、`internal/domain`、`internal/application`、`internal/runtime`、`internal/transport`、`internal/platform` 和 `internal/storage` 基础目录
- [x] 2.2 实现单设备领域状态机，覆盖 absent、discovered、connecting、initializing、ready、degraded 和 disconnected 转换
- [x] 2.3 实现 `DeviceDiscovery`、`SerialTransport`、网络控制、包隧道和服务安装端口接口
- [x] 2.4 实现稳定设备身份关联，覆盖物理位置、VID/PID、IMEI、序列号和 USB 重新枚举
- [x] 2.5 实现运行时 worker、后端句柄、关闭流程、拔出取消和重连退避
- [x] 2.6 实现 AT、QMI、MBIM、APDU、网络和 VoWiFi 资源锁及取消语义
- [x] 2.7 实现 capability 快照和运行时事件总线，保证只有运行时拥有设备生命周期状态
- [x] 2.8 为生命周期、重新枚举、操作冲突、取消和初始化失败增加无硬件测试

## 3. AT/QMI/MBIM 后端

- [x] 3.1 定义 `ModemBackend` 业务端口及 Identity、Radio、SIM、SMS、USSD、APDU、Capabilities、Events 和 Close 契约
- [x] 3.2 将现有 AT manager 接入 backend 生命周期、能力映射、超时、事件和关闭接口
- [x] 3.3 将现有 MBIM 实现接入统一 backend，支持无 AT 端口初始化和 MBIM 指示消息
- [x] 3.4 盘点并接入 QMI NAS、WMS、UIM、DMS、数据连接和事件订阅能力
- [x] 3.5 实现设备配置、接口探测和 capability 协商驱动的 backend 选择，并记录选择原因
- [x] 3.6 让 eSIM/APDU、短信、网络和 VoWiFi 服务依赖能力端口而不是 AT 具体类型
- [x] 3.7 为 AT、QMI、MBIM 增加协议模拟响应、最小成功路径、超时、断线和不支持能力测试
- [x] 3.8 验证 MBIM-only 和 QMI-only 设备不因缺少 AT 串口而启动失败

## 4. Application Services

- [x] 4.1 实现设备状态 application service，输出身份、ICCID、IMSI、SIM、注册、信号、运营商、制式、backend 和 capability
- [x] 4.2 抽取短信服务，覆盖读取、发送、长短信重组、验证码展示、发送结果和必要清理
- [x] 4.3 抽取 SIM/eSIM 服务，覆盖 APDU、EID、Profile 查询、下载、启用、改名、删除和操作进度
- [x] 4.4 抽取网络服务，覆盖 USB 模式、网卡、地址、默认路由、流量和连通性诊断
- [x] 4.5 抽取 raw AT 调试服务，仅在 `raw_at` capability 存在时接受命令
- [x] 4.6 为所有长操作实现 operation manager、状态持久性边界、进度通知、超时和取消
- [x] 4.7 使用内存 backend、fake transport 和故障注入覆盖离线、端口冲突、网络不可用和模组重置

## 5. REST 与 WebSocket API

- [x] 5.1 建立 `/api/v1/device`、`/sms`、`/esim`、`/network`、`/vowifi` 路由组及 DTO 转换
- [x] 5.2 实现统一鉴权边界、参数校验、错误转换和 `capability_not_supported` HTTP 响应
- [x] 5.3 实现 OpenAPI 文档和 API 契约测试，覆盖查询、命令、异步 operation_id 和失败响应
- [x] 5.4 实现 `/api/v1/events/ws` 认证、连接管理、快照首发和事件订阅
- [x] 5.5 实现带 id、type、version、occurred_at、data 的事件封套和断序检测/快照重同步
- [x] 5.6 将设备、SIM、短信、eSIM、backend、网络和 VoWiFi 事件接入统一广播器
- [x] 5.7 删除旧 macOS 路由和入口，确认统一 `/api/v1` 与 `cmd/djonehub` 接管全部功能

## 6. Vue 管理端

- [x] 6.1 创建根目录 `web/` 的 Vue 3 + TypeScript + Vite 工程，锁定构建与类型检查依赖
- [x] 6.2 建立 API service、DTO 类型、Pinia stores、WebSocket client、快照重同步和 operation 状态模型
- [x] 6.3 实现设备状态首页，展示连接、SIM、信号、注册、运营商、backend、模式和 offline 状态
- [x] 6.4 实现短信页面，覆盖收件箱、发送、长短信、验证码和清理操作
- [x] 6.5 实现 eSIM 页面，覆盖 EID、Profile、下载、启用、改名、删除和进度
- [x] 6.6 实现网络、AT 调试和 VoWiFi 页面，并按 capability 控制操作和不支持原因展示
- [x] 6.7 验证无硬件启动、事件断线重连、异步进度、错误展示和 Vue 类型检查
- [x] 6.8 在 Vue 完成核心页面迁移后删除旧 macOS 页面，确认功能只由 Vue 管理端提供

## 7. VoWiFi/IMS 生命周期

- [x] 7.1 创建 `internal/vowifihost` 或等价 service，将启用、禁用、重连和恢复接入单设备运行时
- [x] 7.2 接入 SIM/ISIM AKA、设备身份、网络和 packet tunnel 端口，禁止 API handler 直接管理 `vowifi-go` session
- [x] 7.3 实现 ePDG、SWu/IKE、IMS 注册、隧道和恢复状态的事件映射
- [x] 7.4 覆盖设备拔出、SIM 切换、网络变化、模组重启、命令过期和失败恢复测试
- [ ] 7.5 在 Linux 实现并验证 VoWiFi/IMS 数据面及 TUN/XFRM 所需适配
- [ ] 7.6 在 macOS 和 Windows 实现控制面/状态 capability 与明确失败提示，不宣称未验证数据面

## 8. 平台适配与发行

- [ ] 8.1 实现 Linux 设备发现、串口/USB、网络控制、QMI/MBIM、netlink、TUN/XFRM 和 systemd/Docker 适配
- [ ] 8.2 实现 macOS DJI/Quectel USB 身份、IOKit/libusb、串口、网络控制、launchd 和安装包适配
- [ ] 8.3 实现 Windows SetupAPI、COM/WinUSB、MBIM、网络 API、Windows Service 和安装基础适配
- [x] 8.4 为每个平台注册真实 capability、日志目录、配置目录、权限和数据目录
- [ ] 8.5 建立 Linux、macOS、Windows 构建、签名、校验和 SBOM 流程，并记录交叉编译不等同于硬件验证
- [x] 8.6 为平台未实现的 discovery、network、packet tunnel 和 service 操作返回结构化不支持错误

## 9. 验证与交付

- [x] 9.1 运行 Go 单元、协议、应用服务、生命周期、API 和事件契约测试，并修复架构边界违规
- [x] 9.2 运行 Vue 依赖安装、类型检查、构建、API service 和 WebSocket 重连测试
- [x] 9.3 使用 fake hardware 覆盖设备离线、发现、连接、拔出、重新枚举、三种 backend 和长操作流程
- [ ] 9.4 在 macOS 记录 DJI/Quectel 设备发现、AT、短信、eSIM、USB 网络和网络诊断回归结果（需单独授权的真实硬件验证）
- [ ] 9.5 在 Linux 记录 AT、QMI、MBIM、短信、eSIM、网络和 VoWiFi 真实硬件结果
- [ ] 9.6 在 Windows 记录设备发现、COM/MBIM、基础短信和网络能力；未验证 eSIM/VoWiFi 不标记完成
- [x] 9.7 检查 API、页面、目录和验收清单中没有设备池、代理、通知、机器人和完整 VoHive 后台功能
- [x] 9.8 更新完成定义、能力矩阵、已知限制和迁移回滚说明，确认所有规格场景均有实现或明确缺口
