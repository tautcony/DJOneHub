# 更新记录

## 2026-08-11 · vOWiFi 集成点接线（IMS 注册器/国家前置代理/eSIM 联动/卡策略）

- **IMS 注册器接线**：`RuntimeStartRequest` 增加 `IMSRegistrar` 透传（请求级优先、回退 Manager 注入值）；新增 `buildIMSRegistrar()`（默认隧道 DNS → SRV 解析 P-CSCF；env 覆盖 `DJONEHUB_VOWIFI_IMS_REGISTRAR` / `DJONEHUB_VOWIFI_IMS_SERVER`）；`Service.SetIMSRegistrar` 运行时刷新。真实 SIP 注册使 `State().IMSReady` 反映实际结果（不再占位）。
- **国家前置代理**：复制上游 `internal/upstreamproxy`（MCC 国家表 + SOCKS5 自检，AGPL-3.0 已登记 notices）；storage 新增 `upstream_proxies`/`upstream_proxy_country_rules` 表与 CRUD；`PrepareStart` 按 MCC 命中规则、`BeforeStart` 探测自检（失败拒启）；管理 API：`/api/v1/vowifi/proxies`、`/api/v1/vowifi/proxy-country-rules`、`/api/v1/vowifi/country-table`；启动初始化国家表缓存。
- **eSIM 切卡联动**：`esim.enable/esim.disable` 操作内 SwitchBegin（切卡前抢占拆除）/SwitchEnd（成功后以 AllowSwitch 恢复）；`IsSwitching` 改查活跃切卡操作（`operation.HasActiveKind`），切卡期间 `Enable` 被拒。
- **卡策略门控**：`vowifi_card_policies` 命名空间持久化卡片级 VoWiFi 开关（默认允许）；`shouldReconcileVoWiFi` 增加策略门控（关闭时清除退避并跳过）；管理 API：`/api/v1/vowifi/card-policies`。
- openspec `vowifi-lifecycle` 规格更新（变更 `2026-08-11-vowifi-lifecycle-manager` 已归档）：实现载体更新为 Manager + 适配器，新增期望态自动拉起需求。
- **前端（Web 管理界面）**：VoWiFi 页新增管理区（tab 形式，如设置页）——「国家前置代理」（含 MCC 国家表就绪状态）、「国家规则」绑定、「卡片策略」VoWiFi 开关（`VowifiView.vue` + `views/vowifi/` 子组件 + `api.ts` 封装 + i18n 中英）。
- **前端可用性判断修正**：VoWiFi 控制按钮原依赖 `vowifi_control` 能力（后端从未实现该接口，恒禁用），改为**设备就绪 + 后端有 `apdu`（SIM AKA）能力**为准；无 APDU 时禁用按钮并提示（`device store` 新增 `ready` 成员）。
- 验证：`go build/vet ./...` 通过；前端 `typecheck`/`lint`/`build`/测试通过；新增/更新测试覆盖 IMS 注册器透传、代理存储 CRUD、`HasActiveKind`/`IsSwitching`、切卡联动、卡策略；管理 API 端到端冒烟验证。

## 2026-08-11 · 同步上游完整 VoWiFi（vOWiFi）功能

- 引入 vowifi-go 引擎（`third_party/vowifi-go`，AGPL-3.0）并移植上游完整生命周期管理器 `internal/vowifihost`：LifecycleController（按设备串行化 + generation 过期拒绝 + 抢占）、RuntimeStore（epoch/claim 启动会话防重）、DesiredRecoverStore（期望态恢复退避）、StateHub 状态发布订阅、teardown 编排（恢复短信模式/射频）。
- 复制 `internal/sim`（SIM AKA provider：APDU 逻辑通道 + MBIM Auth 回退）。
- 新增宿主适配器（`internal/application/vowifi`）：`runtimehost.Modem`（AT/QMI/MBIM 双路径）、启动画像构建（实时 IMSI/归属 MCC/MNC）、SIM AKA 注入、飞行模式切换、启动失败处理（APDU busy 3/5/10s 退避）、期望态自动拉起（启动 5s + 30s 对账）。
- 原 7 态简化 `Host` 状态机替换为 Manager；HTTP `/api/v1/vowifi` 路由与前端 `vowifi.state` 展示契约保持兼容。
- 配置：`DJONEHUB_VOWIFI_ENABLED=1` 启用期望态自动拉起。
- 后续集成点（未实现）：语音网关（voicehost）、IMS 注册器、SMS/voice 事件分发、e911、国家前置代理。

## 2026-08-01 · v0.1.5-preview（4G 自动联网修复）

- 新增“模块重连后自动续租 DHCP”：模块 USB 重连、AT 桥重新打开后，自动检查 4G 网卡（Baiwang）是否获得有效 IPv4 地址；没有则自动执行 `networksetup -setdhcp` 续租并等待，最多 30 秒，无需手动重启模块。
- 修复场景：模块掉线重连后 USB 网卡 `en8` 链路恢复但 DHCP 无响应，导致 Wi-Fi 断开时无法自动切换 4G 上网。
- 旧版本均保留：v0.1.4-preview（Windows 实验版）、v0.1.3-preview（macOS 通用）、v0.1.2 / v0.1.1-preview（macOS arm64）。


## 2026-08-01 · v0.1.4-preview（Windows 实验版）

- 新增 Windows 版可执行文件 `DJOneHub-Windows-amd64-v0.1.4-preview.exe`：amd64 单文件，Web 管理界面已内嵌，解压后直接运行，无需安装。
- 通过串口（虚拟 COM 口）连接的 DJI 4G 模块功能可用：短信、GPS、来电提醒、网络信息、Web 管理面板。
- **已知限制**：USB 直连 AT 桥依赖 macOS + libusb，Windows 上不可用，eSIM 管理与 USB AT 通道受限。
- **风险提示**：Windows 版为实验性构建，仅在 macOS 上交叉编译验证，未在真实 Windows + 模块环境实测，可能出现问题，请谨慎下载使用。
- 旧版 macOS 安装包（v0.1.1 / v0.1.2 / v0.1.3）均保留，未覆盖。


## 2026-07-31 · v0.1.3-preview（支持 Intel Mac，通用安装包）

- 新增通用（universal）安装包 `DJOneHub-macOS-universal-v0.1.3-preview.dmg`：一个安装包同时支持 Apple Silicon（M 系列）与 Intel（x86_64）Mac，macOS 13 及以上。
- 主程序、libusb 运行库、通知助手均为 arm64 + x86_64 双架构。
- 本版本不包含菜单栏网速显示（与 v0.1.2 一致）。
- **风险提示**：通用包仅在 Apple Silicon 上交叉编译并验证架构/签名，**未在真实 Intel Mac 上实际测试**，在 Intel 机型上可能出现兼容性问题，请谨慎下载使用。


## 2026-07-31 · v0.1.2-preview（移除菜单栏网速显示）

- 移除菜单栏“实时下载/上传速度”显示，菜单栏只保留 GPS 与 4G 信号图标。
- 管理页面内的实时网速与本次流量统计不受影响。
- 来电/短信提醒、GPS 面板、4G 信号图标等行为保持不变。
- 如果你更喜欢菜单栏显示实时网速，可继续使用 v0.1.1-preview 安装包（旧版保留，未覆盖）。


## 2026-07-30 · GPS 面板与菜单栏信号

本批更新让模块状态在菜单栏直接可见：GPS 定位面板、4G 信号与实时网速。

### 新增

- **原生 GPS 地图面板**：点击菜单栏 GPS 图标打开浮动面板，展示当前位置、卫星数与 HDOP 等定位详情；定位搜索带动画，超时后自动停止扫描并加快状态恢复。面板采用“控制中心卡片”样式，并新增总览玻璃卡片。
- **菜单栏 4G 信号图标**：USB 4G 接管默认网络时，菜单栏显示四格信号与“4G”标识，点击直达控制面板；图标方案预览见 `design-previews/cellular-status-icon-styles.html`。
- **菜单栏实时网速**：显示当前默认网络的下载/上传速率，每秒刷新。
- **菜单栏 GPS 状态指示**：卫星图标与信号格，搜索定位时带动画，超时后自动停止扫描。

### 改进

- 短信面板排版与可读性优化。
- notifier 轮询串行化，避免并发轮询漏掉来电/短信提醒。
