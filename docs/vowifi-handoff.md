# vOWiFi 后续实现功能 Handoff

> 2026-08-11 · 在完成上游 vOWiFi 完整功能同步（vowifi-go 引擎 + `internal/vowifihost` 管理器 + 宿主适配器）后编写。本文记录当前版本明确未实现、需后续接线的集成点，供接手者按序推进。
>
> 2026-08-11 同日第二轮实施：**第 1 项（IMS 注册器接线）、第 5 项（国家前置代理）、第 6 项（eSIM 切换联动）、第 7 项（卡策略门控）、第 8 项（openspec 规格更新）已完成**（见各节"✅ 已实施"标注）；第 2/3/4/9 项仍待推进。

## 现状速览

已落地（本仓库内，均通过构建与测试）：

| 组件 | 位置 | 说明 |
| --- | --- | --- |
| vowifi-go 引擎 | `third_party/vowifi-go/` | AGPL-3.0，vendored，`go.mod` replace |
| 生命周期管理器 | `internal/vowifihost/` | 自上游逐字复制（含测试），唯一仓库内依赖 `pkg/logger` |
| SIM AKA provider | `internal/sim/` | 自上游逐字复制（含测试） |
| 宿主适配器 | `internal/application/vowifi/host_adapter.go` | `vowifihost.Adapter` + `runtimehost.Modem`（AT/QMI/MBIM 双路径） |
| 服务门面 | `internal/application/vowifi/service.go` | Manager 封装 + 期望态自动拉起（启动 5s + 30s 对账） |
| 配置 | `DJONEHUB_VOWIFI_ENABLED=1` | `vowifi.ConfigFromEnvironment()` |

**关键事实**：上游 vohive-open 自身也未实现下面第 1、2 项 —— `req.IMSRegistrar == nil` 时引擎直接报告 `IMSReady: true` 而不实际注册 IMS。因此当前"已注册"状态是占位值，真实 IMS 注册是引擎层待实现功能，不是纯接线。

---

## 1. IMS 注册器（WireIMSRegistrar）— ✅ 已实施（2026-08-11）

- 接线：`internal/vowifihost/runtime_start.go` 的 `RuntimeStartRequest` 已增加 `IMSRegistrar` 字段并在 `StartRuntime` 透传（请求级优先，回退 Manager 注入值）；`Manager.ConfigureIMSRegistrar` + `Service.SetIMSRegistrar`（re-configure 模式）
- 构造：`internal/application/vowifi/ims_registrar.go` `buildIMSRegistrar()` — 默认走引擎解析路径（隧道 DNS → SRV 解析 P-CSCF，参考 `third_party/vowifi-go/runtimehost/imsregistrar.go`），env 覆盖 `DJONEHUB_VOWIFI_IMS_REGISTRAR` / `DJONEHUB_VOWIFI_IMS_SERVER`
- **仍待验证**：真实 SIP REGISTER 事务需测试 IMS 环境；`State().IMSReady` 反映真实注册结果的代码路径已打通（引擎 `types.go:346` 起）
- **验收/风险（原条目）**：验收 = 引擎日志出现真实 SIP REGISTER 事务、`IMSRegistrationResult.Registered=true`；风险 = SIP 注册对网络环境（ePDG 可达性、代理）敏感，建议先在有前置代理/测试 IMS 的环境验证

## 2. 语音网关（voicehost.Gateway）

- **引擎参考**：`third_party/vowifi-go/runtimehost/voicehost/` — `Gateway.RegisterAgent(deviceID, inst)`；`StartRequest.VoiceGateway`；`VoiceMediaRelay *voicehost.RTPRelayConfig`
- **上游模式**：`internal/device/pool_vowifi_wiring.go` 的 `SetVoiceGateway` 持有网关，每次设置后重新调用 `ConfigureRuntimeDependencies(vg, ds, ed)`；main 在网关构造后注入
- **现状**：`Service.NewService` 里 `manager.ConfigureRuntimeDependencies(nil, nil, nil)`（`service.go` 注释标注了后续集成点）
- **实现步骤**：
  1. 构造 `voicehost.Gateway`（engine 内类型，需确认构造方式/所需网络面）
  2. `Service.SetVoiceGateway(g)` → 重新 `ConfigureRuntimeDependencies`
  3. 前置依赖：**第 1 项 IMS 注册器**（voice agent 需要 `IMSRegistrationResult` 的 binding/transport）
- **验收**：`Instance` 有 voice agent 后，`StartOutboundCall/EndVoiceCall` 等 API 可用（`runtimehost/types.go` 542-621 行）
- **注意**：DJOneHub 无音频/RTP 基础设施，`RTPRelayConfig` 与音频输出需另行设计

## 3. SMS / 事件分发（DeliveryStore + Dispatcher）

- **引擎参考**：`runtimehost/messaging.DeliveryStore`、`runtimehost/eventhost.Dispatcher`；事件类型 `SMSReceived/SMSSent/LocalNumberLearned/USSDUpdated/LogNotify`（`runtimehost/types.go` 1132-1138 行别名）
- **上游参考**（可直接复制再适配）：
  - `internal/device/vowifi_delivery_store.go` — 依赖 `internal/db`（gorm）的 SMS 投递表；DJOneHub 需改写为 `internal/storage`（`SQLiteStore` + `database.Namespace(...)`）
  - `internal/device/vowifi_dispatcher.go` — 依赖 `internal/smsnotify`；分发到短信历史入库 + 通知
  - `internal/device/vowifi_sms_history_recorder.go` — 入站/出站短信记录
- **现状**：`ConfigureRuntimeDependencies(nil, nil, nil)`；SMS 走调制解调器 AT/QMI 路径（`internal/application/sms`），与 VoWiFi IMS SMS 无关
- **实现步骤**：
  1. 复制 dispatcher/recorder → 适配 DJOneHub 的 `sms.Service`（历史入库）与 `notify`（通知渠道）
  2. delivery store 改写为 storage 层实现
  3. `Service` 构造时注入 `ConfigureRuntimeDependencies(nil, store, dispatcher)`
- **前置依赖**：第 1 项（SMS 经 IMS transport 收发）；前置代理（第 5 项）影响 SIP 路径可达性
- **验收**：IMS 短信到达 → `sms.Service` 历史出现记录 → 通知渠道触发

## 4. e911 紧急呼叫

- **上游参考**：`internal/e911/`（entitlement 获取、EAP-AKA 鉴权、地址更新、挑战应答；依赖 `runtimehost/e911` + `runtimehost/carrier`）
- **注意**：与 DJOneHub 的固件 EDL（emergency download mode，`internal/runtime/edl_session.go`）是两回事，无冲突
- **现状**：DJOneHub 无 e911；引擎包内已有 `runtimehost/e911`
- **实现步骤**：移植 `internal/e911` 包（entitlement 状态需持久化 → storage）→ HTTP API 接线 → 与呼叫流程（第 2 项）联动
- **优先级**：低（依赖 1、2 就位后才有意义）

## 5. 国家前置代理（upstreamproxy）— ✅ 已实施（2026-08-11）

- 复制：`internal/upstreamproxy/`（`country_table.go` + `probe.go` + 测试，逐字复制，stdlib-only；来源已登记 `THIRD_PARTY_NOTICES.md`）
- 存储：`internal/storage/upstream_proxy.go` — 新表 `upstream_proxies` + `upstream_proxy_country_rules`（configure 批建表，无需 migration 版本）+ CRUD / `GetHomeMCCUpstreamProxy`
- 管理 API：`GET/POST/DELETE /api/v1/vowifi/proxies`、`/api/v1/vowifi/proxy-country-rules`、`GET /api/v1/vowifi/country-table`（`internal/api/http/vowifi_admin.go` + openapi）
- 接线：`Service.SetStore` → `hostAdapter.PrepareStart` 的 `resolveVoWiFiCountryProxy`（MCC→国家→代理）+ `BeforeStart` 的 `ProbeSOCKS5` 自检（5s，失败拒启）；app 启动时 `InitCountryTable`（缓存 `~/.config/DJOneHub/mcc-mnc-table.json`）
- **验收（原条目）**：命中国家规则时引擎隧道走 SOCKS5 代理；代理不可达时启动失败并有清晰错误

## 6. eSIM 切换联动（SwitchBegin / SwitchEnd）— ✅ 已实施（2026-08-11）

- 实现：`operation.Manager.HasActiveKind(kinds...)`（新增）；`hostAdapter.IsSwitching` → 活跃 `esim.enable/esim.disable` 检查
- 联动：`vowifi.Service.SwitchBegin/SwitchEnd` 透传（固定设备槽）；`esim.Service.SetVoWiFiSwitcher` 最小接口 + Enable/Disable op 内 SwitchBegin（前）/SwitchEnd(ctx,true)（成功后）——失败 warn 继续、不自动恢复；锁域注释确认无死锁（esim op 持 ResourceSIM，lifecycle 命令不取 runtime 资源）；app.go 接线
- **验收（原条目）**：切卡期间 `Enable` 被拒（"正在切卡"）；切卡完成后 VoWiFi 自动恢复；进行中的 enable 被正确抢占

## 7. 卡策略（card policy）接入期望态 — ✅ 已实施（2026-08-11）

- 实现：`internal/application/vowifi/card_policy.go` — `database.Namespace("vowifi_card_policies")` JSON map（`CardPolicyStore`），**默认允许**（无策略行 = 允许，保持未接线前行为；上游默认拒绝，有意差异）
- 门控：`shouldReconcileVoWiFi` 增加策略检查 → 关闭时 `ClearDesiredRecoverState` + `VOWIFI_DESIRED_RECOVER_SKIPPED_CARD_POLICY`；`Disable` 后由 30s 对账按策略自然处理（上游 `applyCardPolicyAfterVoWiFiDisable` 的射频恢复语义已由 `manager.Disable` 覆盖，未单独实现）
- 管理 API：`GET /api/v1/vowifi/card-policies`、`PUT ?iccid=`（body `{vowifi_enabled}`）
- **验收（原条目）**：卡策略关闭时期望态/事件恢复被跳过且退避状态被清除；开启后自动拉起

## 8. openspec 规格更新 — ✅ 已实施（2026-08-11）

- 已归档变更 `2026-08-11-vowifi-lifecycle-manager`：`openspec/specs/vowifi-lifecycle/spec.md` 需求 1/4 正文更新为 Manager + 适配器实现载体；新增"期望态自动拉起（5s 启动 + 30s 对账 + 单飞退避）"需求；其余需求未动（独立变更，未与其他功能混用）

## 9b. macOS USB AT 接入共享 AT 后端 — ✅ 已实施（2026-08-11）

**现象**（2026-08-11 真机前置验证时发现）：macOS + USB 直连模块点击 enable 失败：
`operation failed type=vowifi.enable error=capability_not_supported details=map[capability:apdu operation:vowifi_prepare_start ...]`；
前端提示"当前后端不支持 VoWiFi：缺少 SIM APDU / MBIM AKA 能力"（`vowifi.unavailableApdu`）。

**根因**：`hostAdapter.PrepareStart` 第一行 `RequireCapability(CapabilityAPDU, "vowifi_prepare_start")`（`internal/application/vowifi/host_adapter.go:141`）。旧 macOS USB 路径有独立的 command backend，导致它与串口 AT 路径的命令、能力和生命周期行为分叉。

| 后端 | SIMAuthProvider | 场景 |
| --- | --- | --- |
| ATBackend（`at_backend.go:562`，AT+CCHO/CCHC/CGLA） | ✅ | linux/windows 串口路径（`at_factory.go:71`） |
| QMIBackend（`qmi_backend.go:1052`，UIM 逻辑通道） | ✅ | QMI 模式 |
| MBIMBackend（`mbim_backend_simauth.go`，MBIM Auth） | ✅ | MBIM 模式 |
| ATBackend + modem.Manager | ✅ | **macOS USB、Linux 串口、Windows 串口** |

**实现方式**：macOS adapter 只打开 libusb AT transport。`ATFactory` 将该 transport 注入 `modem.Manager`，再统一创建 `ATBackend`、eSIM port 和设备级 `apduarbiter`。Linux 和 Windows 继续由同一 factory 打开串口。三个平台共用 AT 命令队列、URC 分流、提示符、超时隔离和 SIM authentication。

**实施结果**：
1. `internal/modem/transport.go`：定义共享的字节流 AT transport contract 和注入式 manager 构造器。
2. `internal/backend/at_factory.go`：为 serial 和 injected transport 统一创建 `ATBackend`。
3. `internal/platform/darwin/usb_at_darwin.go`：让 libusb transport 复用 Manager 的命令状态机。
4. 测试：覆盖 CCHO/CGLA/CCHC 命令构造、通道号解析、能力声明、仲裁租约和 injected transport 生命周期。
5. 启动失败可见性：失败状态保留 `last_error` 和 `reason`。前端 VoWiFi 面板和异步操作卡片显示具体错误。

**验收**：代码和合成 transport 测试已验证能力、命令和仲裁路径。macOS 真机上的 `/api/v1/vowifi` available、enable 状态流转和前端按钮状态仍需按第 9 项清单执行硬件验证。
**风险**：模块不支持 CCHO/CGLA 时需回退（QMI 模式或串口连接）；macOS libusb transport 仍需按 macOS 构建脚本和真实硬件验证。

## 9. 真机端到端验证清单

前置：Linux + EC25/4G 模块（参考 `docs/EC25`），或 macOS + darwin AT 路径。

```bash
DJONEHUB_VOWIFI_ENABLED=1 go run ./cmd/djonehub   # 或对应入口
```

| 验证点 | 预期 |
| --- | --- |
| 启动 5s 自动拉起 | 日志 `VOWIFI_DESIRED_RECOVER`；状态流转 SIM→identity→tunnel |
| `/api/v1/vowifi` | `state` 串（disabled/connecting/connected/failed）、`instance.obs`、`desired_recover` 字段 |
| 飞行模式 | 启动进入 `ModeRFOff`；disable 后 `ModeOnline` 恢复 |
| APDU busy | 3/5/10s 退避自动恢复（日志 `VoWiFi APDU busy`） |
| 期望态 30s 对账 | 掉线后低频拉回；`VOWIFI_DESIRED_RETRY_DELAY` 退避递增（30s/1m/2m） |
| SIM 拔出 | `TeardownForReconnect` 拆除实例；插入后对账拉起 |
| `vowifi.updated` / `vowifi.state.changed` | EventBus 事件持续发布，前端状态同步 |
| 前端管理区（2026-08-11 起） | VoWiFi 页代理/规则/卡策略/国家表状态读写；改配置后重启或重新启用 VoWiFi 生效 |
| 引擎测试 | `go test ./third_party/vowifi-go/... ./internal/vowifihost/... ./internal/sim/...` |

## 推进顺序建议（更新于 2026-08-11 第三轮后）

1. **第 9 项真机验证**（含本轮已实施的 macOS USB SIM APDU 接线，以及 IMS 注册器 `DJONEHUB_VOWIFI_IMS_REGISTRAR`/`_SERVER` 覆盖、代理规则、切卡联动、卡策略）
2. 第 3 项 SMS 分发（依赖第 1 项真实生效）
3. 第 2 项语音网关、第 4 项 e911（低优先）
4. 已实施项（1/5/6/7/8/9b）的验收场景见各节标注，随真机验证一并执行

## 通用注意

- `internal/vowifihost` 是自上游复制的包：**修改前先看上游是否已修**，保持可 diff 性；对 `runtime_start.go` 的 `RuntimeStartRequest` 改动要在包内注释标明
- quectel-qmi-go：上游用 fork replace（`./third_party/quectel-qmi-go`），DJOneHub 用发布版 v0.6.0；若 QMI VoWiFi 指令需要 fork 补丁，复制 fork 并按 `go.mod` 的 "Fork patch note" 惯例注释
- 新增依赖进 `THIRD_PARTY_NOTICES.md` 维护（vowifi-go 已登记，AGPL-3.0）
