# DJOneHub 全仓库代码复查报告

- **日期**: 2026-08-06
- **范围**: 全仓库（Go 后端、web 前端、macOS Swift 原生 UI、构建脚本）
- **方法**: 按模块划分 10 个并行 subagent，每个模块独立从设计、架构、安全、正确性、并发、资源管理、错误处理 7 个维度深度复查
- **基线**: `main` @ `965b624`

---

## 1. 摘要

| # | 模块 | Critical | High | Medium | Low | 合计 |
|---|------|---------:|-----:|-------:|----:|-----:|
| 1 | eSIM 子系统（esim/simaid/apduarbiter） | 0 | 2 | 7 | 9 | 18 |
| 2 | modem（AT 通道/ModemManager） | 0 | 4 | 11 | 6 | 21 |
| 3 | backend（AT/QMI/MBIM 后端） | 0 | 7 | 9 | 3 | 19 |
| 4 | application（用例层） | 0 | 2 | 5 | 11 | 18 |
| 5 | platform（Linux/macOS/Windows 适配器） | 0 | 0 | 6 | 12 | 18 |
| 6 | api/storage/config/app/runtime 等 | 1 | 2 | 5 | 4 | 12 |
| 7 | pkg（mbim/smscodec/logger） | 0 | 2 | 3 | 5 | 10 |
| 8 | web 前端（Vue3/TS） | 0 | 1 | 4 | 5 | 10 |
| 9 | macOS Swift 原生 UI | 0 | 1 | 5 | 6 | 12 |
| 10 | 入口与跨模块架构 | 0 | 1 | 4 | 6 | 11 |
| | **合计** | **1** | **22** | **59** | **67** | **149** |

> 说明：部分发现被多个模块独立确认（如本地 API 无鉴权在 #4/#6/#8/#10 均被报告），本报告已在"跨模块共性问题"中归并去重，模块章节中保留各自定位的上下文。

### 最紧急问题（按修复优先级排序）

| 优先级 | 问题 | 影响 | 涉及位置 |
|--------|------|------|----------|
| P0 | 本地 HTTP API 完全无鉴权 + WebSocket 无 Origin 校验 | 任意恶意网页可读短信/通话/设备身份，发短信、删 eSIM、执行任意 AT、开交互式 ADB shell | `internal/app/app.go:211`、`internal/api/http/server.go:121,628,1037` |
| P1 | SMS 链路整体不可用：入站短信读后即删且无消费者、索引伪造、PDU 未解码、长短信跨轮重组丢失 | AT/QMI 路径收件箱永远为空、消息错读、多段短信永久丢失 | `internal/modem/manager.go:1395`、`internal/backend/at_backend.go:325`、`internal/application/sms/service.go:338` |
| P1 | AT 应答错位：超时后残留不冲刷、`>` 误判提示符 | 下一条命令拿到上一命令的应答，状态查询返回垃圾数据 | `internal/modem/manager.go:550,596` |
| P1 | eSIM 同步原语竞态：`opDone` 裸竞争、watchdog 强释不取消在途 APDU | 读请求误报"操作进行中"；exclusive 语义被破坏 | `internal/esim/manager.go:951`、`internal/apduarbiter/arbiter.go:441` |
| P1 | 协议解析信任线上 count/length：预分配、分片累积无上限 | 恶意/畸形设备响应可致 OOM、goroutine 泄漏 | `pkg/mbim/sms.go:55`、`pkg/mbim/fragment.go:27`、`internal/modem/manager.go:643` |
| P2 | 事件/通知可靠性：通道满时静默丢事件/命令 | 来电卡片永久卡死、拒接无反馈、漏通知 | `internal/runtime/events.go:31`、`internal/backend/at_backend.go:120`、`macos/.../NativeUIHost.swift:306` |
| P2 | 生命周期不完整：轮询器不可停、operation 无取消、zap 日志从未初始化 | 关停竞态写库、设备层日志全部静默丢弃 | `internal/app/app.go:286`、`pkg/logger/logger.go:17` |
| P2 | vowifihost 端口泄漏 + 无界并发恢复 | 反复失败累积 modem 端口、恢复操作互踩 | `internal/vowifihost/host.go:67,202` |
| P2 | macOS Swift 6 `@MainActor` 通知代理回调崩溃风险 | 通知点击冷启动时进程直接 abort | `macos/DJOneHubNotifier/Sources/DJOneHubNotifier/NativeUIHost.swift:121` |

---

## 2. 跨模块共性问题

以下问题跨越多个模块边界，是系统性缺陷而非单点 bug。

### 2.1 本地控制面没有安全边界（Critical，4 个模块独立确认）

- `internal/app/app.go:211-240` 组装 `httpapi.Config` 时**从未注入 `Auth` 字段**，`server.go:121-127` 的 `protected()` 恒放行；代码里已有 `Authenticator` 接口与 `derrors.Unauthenticated` 分支，但未接线。
- WebSocket 全部不校验 Origin：`events/ws`（server.go:1037）手写 hijack 升级；ADB shell WS 的 `CheckOrigin: func(...) bool { return true }`（server.go:628）。WebSocket 不受 CORS 约束，恶意网页可静默读取实时短信/通话/设备身份（事件流推送未脱敏数据）。
- HTTP 写端点 `decodeJSON`（server.go:1109）不校验 Content-Type，跨站 `text/plain` 简单请求（无预检）即可触发全部写操作：发短信、删 eSIM、`raw-at` 执行任意 AT、固件备份。
- 默认仅绑 `127.0.0.1:7575`，但 `-listen 0.0.0.0:7575` 即无凭据开放整机模组控制面。
- 配置中残留死配置：`web.username/password` 默认 `admin/admin`（`internal/config/config.go:251-252`），但从未启用。

**修复建议**：在 `app.go` 注入基于 loopback + Origin/Host 校验的 `Authenticator`；`adbShellUpgrader` 设置 `CheckOrigin` 白名单；写端点校验 Content-Type 或 `Sec-Fetch-Site`；将 `openapi.go` 的 securityScheme 与实际鉴权对齐。

### 2.2 SMS 链路端到端不可用（High，backend + application + modem + pkg 交叉确认）

- `internal/modem/manager.go:1395-1433`：`+CMTI` 到达后自动 `AT+CMGR`+`AT+CMGD` 删除，而 `SetSMSCallback` 全仓库无任何调用方 → 入站短信"读后即删"，收件箱永远为空。
- `internal/backend/at_backend.go:325-336`：`ListSMS` 用循环下标伪造存储索引，真实索引被丢弃 → `ReadSMS`/`DeleteSMS` 读错消息或直接 ERROR。
- `internal/backend/business_adapter.go:116-130` + `at_backend.go:303-317`：`ListSMS` 只保留 Index，下游去重键全相同 → 去重碰撞；`ReadSMS` 把 hex PDU 原样放进 `Body`，无解码 → 用户看到十六进制而非正文。
- `internal/application/sms/service.go:338-356`：长短信重组器**每次轮询新建**，跨轮分片永久丢失（3 秒轮询恰落在分段投递窗口内时必现）。
- `pkg/smscodec/reassembler.go:36-41`：重组缓存 key 不含 Total，8 位 ref 回绕时相邻消息分片互相污染。
- `internal/modem/manager.go:1502-1532`：存储切换（CPMS）跨多条命令非原子，并发 `+CMTI` 下互踩，短信漏读。

**修复建议**：应用层注册 `SetNewSMSHandler` 接管 URC；`SMSListAllPDU` 返回索引+PDU；两个后端 `ReadSMS` 内完成 PDU 解码；`Reassembler` 提升为 Service 持久字段（互斥保护）；存储切换加 per-Manager 流程级互斥。

### 2.3 AT 命令层的应答错位（High，modem + backend 独立确认）

- 命令超时后只发 ESC 不冲刷 `rxChan` 中迟到的应答（`manager.go:550-564`），下一条命令会吞掉残留行（迟到 OK 让命令立即"成功"，迟到数据行拼入 fullResponse）。
- 提示符判定 `strings.Contains(line, ">")`（`manager.go:596-616`）过于宽松：任何含 `>` 的行（如 USSD 菜单文本）都会终止当前命令，且该行不走 URC 分发；`readLoop` 的 `strings.HasSuffix(data, ">")` 同理会清掉半行数据。
- 连续 5 次超时触发 watchdog 直接 `Stop()` 断开设备（`manager.go:337-360`），慢设备可被误判故障触发整机重连。

**修复建议**：超时后短暂 drain；提示符只匹配裸 `"> "` 且仅 `interactive` 命令启用；`isURC` 判定移到 `>` 之前；watchdog 阈值可配置并排除长命令。

### 2.4 事件/通知链路静默丢数据（Medium-High，backend + application + macOS 交叉确认）

- `EventBus.Publish` 对慢订阅者直接 drop（`internal/runtime/events.go:31-37`），notification 订阅（64 槽）的消费者同步调用 Swift Sink，桥接慢时 `call.ended`/`sms.received` 被丢弃 → 来电卡片永不消失、漏通知。
- `ATBackend.Events`（at_backend.go:120-152）在通道满时**阻塞发送**，慢消费者会让整个 AT 命令循环停摆（liveness 问题）；`QMIBackend.Events` 同理阻塞 qmicore 事件派发。
- Go→Swift 命令通道满时 `select default` 静默丢包（`internal/platform/darwin/native/bridge.go:295-299`），拒接命令丢失后 UI 永久卡"拒接中"（Swift 侧 `rejectingCallID` 无超时）。
- 事件解析失败（`BridgeEvent.parse` 返回 nil 即丢弃）、C ABI 无错误码，Go 侧完全无法诊断。

**修复建议**：事件发送一律非阻塞（丢弃时计数并暴露指标）；notification 的 Sink 调用与消费解耦（内部队列 + 独立投递 goroutine），恢复后基于 extras/sms 状态做对账；Swift 侧拒接加超时；桥接增加 `ui.error` 回传。

### 2.5 生命周期与关停不完整（Medium，app + api + application + arch 交叉确认）

- `App.Stop`（app.go:286-290）只停 Notification+Runtime+Store，SMS/Network/Extras 三个轮询器既不停止也不等待，`Store.Close()` 与在途 `InsertSMS`/`RecordTrafficSample` 竞态（"database is closed" 错误被静默吞掉）。
- `operation.Manager` 用 `context.WithoutCancel` 使长操作脱离请求生命周期，且无任何 `Stop`/`CancelAll`，关停时在途固件刷写/备份无法取消。
- HasUI 分支存在双路并发 shutdown：UI 退出与 `<-ctx.Done()` 的 goroutine 会并发执行 `NativeUI.Stop()` + `shutdown()`；`Bridge.send` 对 `started/exited` 无检查，可能在已停止的 AppKit 运行循环上投递事件。
- `events/ws` 无读循环、无读写 deadline：慢/静默客户端永久占用 goroutine 与订阅；snapshot 在 `Subscribe` 之前写出（丢事件窗口），snapshot ID 复用 `LastID()`（客户端按 ID 去重会漏事件）。
- `server.ListenAndServe` 失败时在 goroutine 内 `log.Fatal` 直接 `os.Exit`，跳过一切清理，NativeUI 随后仍启动并指向连不上的 URL。

**修复建议**：轮询器保存 cancel + done channel（参照 `notification.Service.Stop`），`App.Stop` 按启动逆序停止并 join 后再关库；`operation.Manager` 增加 `Shutdown(ctx)`；收敛为单一 shutdown 路径；WS 升级改用 gorilla/websocket（读循环 + deadline），先订阅再发 snapshot 并用 `LastID()+1` 语义。

### 2.6 旧树迁移残留：死代码 + 初始化遗漏（Medium，arch + api + pkg 交叉确认）

- `pkg/logger`（zap）：`Log = zap.NewNop()`（logger.go:17），新入口 `cmd/djonehub`/`internal/app` 从未调用 `logger.Setup` → **设备层（modem/backend/esim）全部 zap 日志静默丢弃**，仅标准库 `log` 可见。
- `internal/config`：`Load`/`GetConfig`/`UpdateNotificationInFile` 等在新二进制中无调用者（viper 全家桶 + Telegram/Feishu/QQ/Webhook/Bark/Email/Pushplus 配置面），是 `vohive-open` 旧库的遗留；`GetConfig` 返回共享指针、`GetDeviceByID` 返回共享数组元素指针，误改即数据竞争；凭据明文 YAML 且目录 `0o755`。
- `pkg/logger` 的 `readerIMSIRegistry` 全仓库无写入者，纯死状态；`internal/esim/pki` 的 `go:generate` 从第三方 URL `curl -sL` 拉取无校验和。
- go.mod 10 条 replace 中 multierr/pkg-errors 与上游逐字节相同（无补丁价值）；根目录与 `vohive-open/` 声明相同 module path，`go build ./...` 会静默跳过旧树。

**修复建议**：在 `main()` 调用 `logger.Setup`；删除无调用者的 config 面并移除 viper 依赖；配置目录改 0700；删除无差异 replace；`vohive-open` 独立 module path；pki 生成加校验和。

### 2.7 敏感信息暴露面偏大（Medium，modem + macOS + eSIM + api 确认）

- modem 在 Info 级明文记录 IMEI/ICCID（`manager.go:787`）、短信内容（1422）、USSD 文本（2060）、拨号/来电号码（286/289）。
- macOS 把完整短信正文（含验证码）放入系统通知，横幅/锁屏/通知中心持久展示，无脱敏选项。
- eSIM 下载日志记录 `matchingID`（激活码组成部分，一次性敏感凭证）。
- `publicText` 用"含 CJK 即整体替换"做脱敏，规则脆弱且 `backend.*` 原始事件、`sms.received` 正文透传。
- 菜单栏 tooltip/右键菜单暴露运营商名与网络模式（低敏感，确认是产品决策即可）。

**修复建议**：IMEI/ICCID 降级 Debug 或脱敏；短信内容/USSD 文本/号码默认不记录或加开关；macOS 通知增加"仅显示 sender"偏好；日志省略 matchingID；脱敏改为显式字段白名单。

---

## 3. 各模块详细发现

### 3.1 eSIM 子系统（internal/esim、internal/simaid、internal/apduarbiter）

#### High

**H1. `opDone` 裸数据竞争 + 丢失唤醒窗口**
- `internal/esim/manager.go:951-958`（notifyWriteDone）、`897-908`（waitForNoWriteOperation）、`921-935`（acquireOperationLock） | 并发
- `m.opDone` 被写者（`opMu.Unlock()` 之后调用）与无锁读者并发读写，race detector 必报。写入顺序为"先替换新 channel、后 close 旧 channel"：读者在两次赋值之间评估 `<-m.opDone` 会捕获永不关闭的新 channel，错过完成通知，阻塞至 5 秒定时器后返回虚假的 `ErrOperationInProgress`——已完成的写操作让读请求报"操作进行中"。
- 建议：持锁（cacheMu）完成"close 旧 → 替换新"，或改用 `atomic.Pointer[chan struct{}]`。

**H2. MaxLeaseHold watchdog 强释租约但不取消在途 APDU，exclusive 语义被破坏**
- `internal/apduarbiter/arbiter.go:441-443`、`702-730` | 并发/正确性
- 2 分钟超时强制释放仍被持有的 transport lease，但不取消在途 APDU。若调用方 ctx 带长 deadline（如 5 分钟），一条 3 分钟的 BPP 安装 APDU 会在 2 分钟时被 force-release，此后新的 exclusive lease 乃至切卡 barrier 都会被授予，与仍在飞行的 APDU 并发打到同一设备。所有 `lease.Touch()` 返回值被忽略，持有方无法感知已被强制释放。
- 建议：force-release 时通知持有方；将 MaxLeaseHold 与单条 APDU 实际上限解耦，或改为"仅对无进展租约生效"语义。

#### Medium

**M1. SIMAuth probe 无 recover，失败后永久卡死**
- `internal/apduarbiter/simauth_ready.go:92-105` | 并发/错误处理
- probe panic 后 `probing` 恒 true、`waitC` 永不关闭，`InvalidateSIMAuthReady` 也不重置，所有 `WaitSIMAuthReady` 调用者阻塞到各自 ctx 到期，无法自愈。
- 建议：defer 清理 probing/waitC，或独立 goroutine + recover。

**M2. 后台 goroutine 无 panic recover**
- `internal/esim/manager.go:1552-1565`（triggerOverviewReload）、`2331-2367`（runPostSwitchHook 等） | 错误处理
- 注释承认 euicc-go 可能对非标准卡片数据 panic；`createLPAWithAID` 与第三方 lpa/bertlv 解析路径不在 recover 覆盖内，panic 直接崩溃进程。
- 建议：加 recover 转日志；`createLPAWithAID` 外层把 panic 转错误。

**M3. 写操作无超时阻塞，长下载让所有并发写无界挂起**
- `internal/esim/manager.go:2068,2223,2978,3033,3255,3307` | 并发/设计
- 所有写操作裸 `m.opMu.Lock()`，读操作有 5 秒超时而写操作没有；API 层得不到忙态反馈，HTTP 侧也无超时兜底。
- 建议：写操作复用 `acquireOperationLock` 的超时语义。

**M4. QMI APDU 超时意图与实现不一致**
- `internal/esim/qmi_uim_transport.go:197-203`、`manager.go:3041,3135` | 正确性/架构
- 注释声称"不再创建固定 10 秒超时"，但 qmiq 客户端在 ctx 无 deadline 时兜底 30 秒——慢 eUICC 的安装 APDU 仍会被截断；API 层传长 deadline 又回到 M3 的无界持有。
- 建议：显式构造"大但不无限"的下载期超时（5-10 分钟）包装注入的 ctx。

**M5. darwin 纯 AT 端口未接入 APDU 仲裁器**
- `internal/esim/at_port.go:24-33`、`manager.go:2037-2056` | 架构
- `NewATPort` 创建 Manager 时未传 `APDUArbiter`，该路径上切卡 barrier 返回 nil、`waitForAPDUIdleForRead` 为 no-op，切卡与 VoWiFi 鉴权冲突窗口未消除。
- 建议：AT 端口共享设备级 arbiter（与 modem.Manager 同一实例）。

**M6. 非 AKA 老化票据可无限饿死 AKA**
- `internal/apduarbiter/arbiter.go:526-571` | 正确性/公平性
- 排队 ≥500ms 的非 AKA transport 优先级高于 AKA；eSIM 读操作周期性重载持续产生 EUICCWrite 请求时，USIMAKA 可被无限期饿死，VoWiFi/SIMAuth 鉴权延迟无上界。
- 建议：AKA 增加绝对等待上限。

**M7. 读路径不可取消**
- `internal/esim/at_port.go:40-56`、`manager.go:1730-1732` | 资源/设计
- `esimPort.Overview/EID/Profiles` 忽略传入 ctx，客户端断开后全量 AID 扫描（每 AID 多条 8-15s 超时 APDU）仍执行数十秒，占用 opMu/仲裁器。
- 建议：读路径传递 ctx 并检查 `ctx.Err()`。

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `internal/simaid/select.go:35-38` | 错误处理 | 全部尝试失败时返回 `(nil, false, nil)`，吞掉 lastErr，无法区分"未匹配"与"传输失败" | 返回 `(nil, false, lastErr)` |
| L2 | `manager.go:770-785,2617-2642,877-878` | 正确性 | 错误分类依赖字符串匹配（isExpectedPostResetLPAClientCloseError 等），文案改动会静默改变判定 | 定义 sentinel error 用 `errors.Is` |
| L3 | `manager.go:3115-3119` | 安全 | 下载日志记录 matchingID（激活码组成部分，一次性敏感凭证） | 日志省略或只记摘要 |
| L4 | `at_channel.go:38,61` | 性能 | 每次 OpenLogicalChannel/Transmit 内 `regexp.MustCompile`（profile 下载透传数百条 APDU） | 提升为包级 var |
| L5 | `internal/simaid/apdu.go:12-15` | 正确性 | `IsSuccess` 把 SW1=0x63（warning/鉴权失败）视为成功 | 仅接受 0x90/0x61/0x62，0x63 单独分类 |
| L6 | `mbim_apdu_transport.go:84-102`、`arbiter.go:902-910` | 设计 | MBIM 每条 APDU 都申请 TransportScopeExclusive，QMI 支持 per-channel 并发，两传输并发模型不一致 | 统一或明确注释 |
| L7 | `qmi_uim_transport.go:78-81`、`apdu_coordinator.go:57-62` | 并发 | Stop 换锁表不等待在途 Transmit，"没有飞行中的 Transmit"注释与实现不符 | WaitGroup 跟踪排空 |
| L8 | `channel.go:57-77` | 正确性 | `ModemChannel.Transmit` 未校验 `c.channel == 0`，APDU 可能打到基本通道；ATSmartCardChannel 有该检查 | 与 at_channel.go 对齐 |
| L9 | `manager.go:366-369` | 设计 | `SwitchProfileResult.SIMReloadWarning`/`PowerCycleAttempt` 死字段，全仓库无写入点 | 删除或实现 |

**模块整体评价**：apduarbiter 的"租约 + barrier + 老化优先级"并发模型抽象清晰，QMI/MBIM 共用 apduCoordinator 分层合理，错误多带上下文包装。系统性弱点有三：手写同步原语存在竞态（opDone、SIMAuth ready）、watchdog/exclusive 语义在长 APDU 下可被破坏、读/写路径在超时与仲裁器接入上不一致（纯 AT 端口整体缺仲裁）。另有两套几乎相同的 AT APDU 封装（simaid 与 esim 的 channel.go）可合并，`AcquireSession/AcquireOneShot` 等 legacy 接口已无生产调用方。

---

### 3.2 modem（internal/modem）

#### High

**H1. EF_IMSI 解码后截断首位数字，MCC 大概率损坏**
- `internal/modem/commands.go:720-723` | 正确性
- `DecodeSwappedBCD` 已还原完整 15 位 IMSI（末尾填充 nibble 被过滤），`imsiStr[1:]` 又按"parity 位在首位"假设砍掉首位——按 3GPP TS 31.102 标准布局这是把 MCC 第一位删掉（如 `460009300011111` → `60009300011111`，NativeMCC=600 无效）。MBIM 路径（`mbim_backend_simfiles.go:125`）不截首位，两条路径行为不一致。已实测：标准布局下截断前即完整正确。
- 建议：真实设备核对 EF_IMSI 字节与 IMSI 对应关系，补单测；若为标准布局删除 `[1:]`。

**H2. 短信存储切换（switch/read/restore）跨多条命令非原子，并发 +CMTI 互踩**
- `internal/modem/manager.go:1381-1430,1502-1532` | 并发/正确性
- 每条 +CMTI 各自 `go readAndProcessSMSFromStorage`，CPMS 切换/读取/恢复之间可插入其他命令，交错后短信漏读且 CPMS 停在错误存储；批量短信到达时极易触发。
- 建议：per-Manager `smsReadMu` 串行化整段流程。

**H3. 命令超时后不冲刷残留响应，污染下一条命令**
- `internal/modem/manager.go:550-564` | 正确性
- 超时只发 ESC、不排空 `rxChan` 中迟到的 OK/ERROR/数据行；下一条命令吞掉残留行（迟到 OK 让下一条立即"成功"，数据行拼入 fullResponse）。命令越快越容易踩中。
- 建议：超时后短暂 drain 或为应答行加命令代际归属。

**H4. 提示符判据 `strings.Contains(line, ">")` 过于宽松**
- `internal/modem/manager.go:596-616`、`readLoop 737-746` | 正确性
- 任何含 `>` 的行（USSD 菜单文本、`+CUSD` 内容）都命中提示符分支：非交互命令被垃圾响应终止，该行还不走 URC 分发；`strings.HasSuffix(data, ">")` 会清掉内容以 `>` 结尾的半行。
- 建议：仅匹配裸 `"> "`/`">"` 且只在 `interactive` 时启用；`isURC` 判定移到 `>` 之前。

#### Medium

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| M1 | `at_parse.go:621-666` | 正确性 | `parseCOPSScan` 用 `Split(line[7:], "),(")` 粗暴切分：尾部的 format/act 列表段（如 `(0,1,2,3,4)`）被解析成幽灵运营商条目（已实测复现）；省略空段时更会解析出 `{PLMN:"3"}` 的假运营商 | 逐段正则/状态机解析，校验 PLMN 5-6 位数字、stat 合法值 |
| M2 | `manager.go:70-74,356-357,457-459,470,1688,1703,217-219,1425,1850,263-268,1346` | 并发 | `running`/`healthy`/`smsCallback`/`qpcmvChan` 无同步访问，`-race` 必报；Stop 后仍可能放行命令 | 收编到 `infoMu` 或 `atomic.Bool` |
| M3 | `manager.go:327,1131-1140,1189-1198,1285-1289,1296-1335` | 并发/资源 | URC 触发的回调/短信读取 goroutine 全部裸 `go`，无 recover，上层回调 panic 直接崩溃进程 | 统一 `safeGo(fn)` 带 recover |
| M4 | `manager.go:787,1422,2043,2060,286,289`；`urc_format.go:105-113,192,205` | 安全 | IMEI/ICCID 明文写 Info；短信内容、USSD 文本、拨号/来电号码进日志 | IMEI/ICCID 降级 Debug 或脱敏；内容默认不记录或加开关 |
| M5 | `manager.go:2036-2070` | 并发 | 并发 `ExecuteUSSD` 无互斥，互相排空对方 `ussdChan`，结果串台；单槽通道只能喂一个等待者 | 加 `ussdMu` 串行化会话，或用会话 ID 匹配 URC |
| M6 | `commands.go:29-43` | 错误处理 | `QuerySIMInserted` 两条路径都失败时返回 `(false, nil)`，把"查询失败"伪装成"未插卡"，`RefreshStatus` 据此误报 SIM 掉线 | 失败时返回错误，分别告警 |
| M7 | `manager.go:282-284,328,370,2049`；`operator_selection.go:49-56` | 安全 | USSD/拨号/PLMN/AID/APDU 参数未白名单校验直接 `fmt.Sprintf` 拼入命令，可注入额外 AT 命令 | 模块边界白名单校验（号码限 `[0-9+*#]`，AID/APDU 限 hex） |
| M8 | `manager.go:490-495` | 错误处理 | runLoop panic 后只记日志就退出循环，无心跳无自愈，后续命令全部卡队列超时 | panic 后置 unhealthy、通知上层重建 |
| M9 | `commands.go:126-136` | 错误处理 | `QueryServingCellLTEInfo` 解析失败返回 `(零值, nil)`，错误被吞，与 CSQ 的 -999 哨兵不一致 | 返回 error 或复用哨兵 |
| M10 | `commands.go:62` | 设计 | 纯查询轮询反复下发 `AT+COPS=3,2`（副作用型读操作），静默改写用户格式选择 | 先解析再按需设置，或调用方传期望格式 |
| M11 | `force_release_linux.go:18-62` | 安全 | fuser 输出与 kill 之间 PID 复用 TOCTOU，误杀无关进程；无进程归属复核（与 platform #5 的发现同源） | kill 前校验 `/proc/<pid>/fd` 确含设备路径 |

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `imei_probe.go:19-52` | 资源 | IMEI 缓存条目永不移除；同一路径换设备后 10 分钟内返回陈旧 IMEI | 周期清理，缓存键加设备指纹 |
| L2 | `manager.go:1706-1717,1779-1788` | 资源 | Stop 清空 APDU 会话登记但不发 `AT+CCHC` 关模组侧逻辑通道（当前无生产调用方） | 清理时尝试 ClearLogicalChannels |
| L3 | `manager.go:1833-1857` | 并发 | `CheckAllSMS` 的 IsBusy 检查与后续操作非原子（TOCTOU，后果较轻） | 检查与操作纳入同一互斥 |
| L4 | `operator_selection.go:25`、`manager.go:500-533` | 设计 | `ScanOperators` 90 秒占住 runLoop，高优先级命令饥饿；URC 洪峰可打满 rxChan 丢行 | 长命令期间周期性处理 rxChan 或独立通道 |
| L5 | `manager.go:542,558,608` | 并发 | 三个 Write 点错误全部忽略，Stop 并发关端口后写失败不计入超时连击 | 写失败统一走 fatal 通道 |
| L6 | `manager.go:1656` | 性能 | `executeAT` 每次调用 `time.After(5s)`（轮询热路径） | 复用 `time.NewTimer` + defer Stop |

**模块整体评价**：最大优点是"单写者"设计——runLoop 串行执行命令、命令执行期间内联分发 URC、优先级队列 + 请求池 + 超时看门狗，并发骨架自洽。主要短板是 2176 行的 Manager god-object 把串口传输、AT 协议、短信/USSD/语音/运营商/APDU 仲裁全部揉在一起，状态用散落 bool/计数器建模而非状态机；modem 反向感知 QMI/MBIM 配置（`pureQMIBackend`）属分层倒挂。测试集中在纯解析函数，对 runLoop/URC 交错/超时恢复等核心时序路径缺乏覆盖。

---

### 3.3 backend（internal/backend）

> 注：backend 的多数委托目标位于 `internal/modem/`，本次审查已覆盖（见 3.2），两处需同步修复。部分 High 项与 SMS 链路相关，已在 2.2 归并。

#### High

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| H1 | `internal/modem/manager.go:1395-1433` | 正确性/数据丢失 | `+CMTI` 自动 `AT+CMGR`+`AT+CMGD` 读后即删，`SetSMSCallback` 全仓库无调用方 → 入站短信静默丢失，收件箱永远为空 | 无消费者时不删除；由应用层接管回调 |
| H2 | `at_backend.go:325-336` | 正确性 | `ListSMS` 用循环下标伪造索引（`+CMGL` 真实索引已丢弃），下游 `ReadSMS`/`DeleteSMS` 读错消息或直接 ERROR | `SMSListAllPDU` 返回索引+PDU 对 |
| H3 | `business_adapter.go:116-130`、`at_backend.go:303-317`、`qmi_backend.go:883-897` | 正确性 | `ListSMS` 只保留 Index（下游去重键全相同 → 去重碰撞）；`ReadSMS` 把 hex PDU 原样放 `Body`，无解码，用户看到十六进制 | 后端内完成 PDU 解码，ListSMS 携带 ReceivedAt |
| H4 | `internal/modem/manager.go:643-745` | 资源/安全 | 行缓冲 `lineBuf` 无上限，异常设备持续发不含 `\n` 的数据可无限增长，超长行还进 fullResponse | 行缓冲设上限（如 4KB），连续超限判设备异常 |
| H5 | `internal/modem/manager.go:592-606` | 正确性/并发 | 应答判定顺序 OK→ERROR→`>`→URC，含 `>` 的 URC 行被当提示符，真实应答留在 rxChan 被下一条命令消费 | 提示符分支只在 interactive+waitPrompt 生效（与 3.2 H4 同源） |
| H6 | `internal/modem/manager.go:559-569,337-360` | 正确性/错误处理 | 超时后不消费迟到应答，下一条命令吞残留；连续 5 次超时直接 Stop 断设备，慢设备误判故障 | 超时后 drain；watchdog 阈值可配置 |
| H7 | `at_backend.go:120-152`、`qmi_backend.go:185-212` | 并发（liveness） | 事件推送在通道满时阻塞发送，慢消费者让整个 AT 命令循环停摆；QMI 同理阻塞事件派发 | 非阻塞发送 + 丢弃计数 |

#### Medium

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| M1 | `command_backend.go:354-356` | 资源 | `CommandBackend.Events` 返回永不关闭的 unbuffered channel，与 `closedBackendEvents()` 约定不一致，每次连接残留 goroutine | 返回 `closedBackendEvents()` |
| M2 | `command_backend.go:158-161`、`contracts.go:41-49` | 正确性 | `ListSMS` 按 SMSC 时钟排序，而契约注释明确该时钟不可靠，跨时区/坏时钟下顺序错乱 | 用本地记录时间 |
| M3 | `command_backend.go:241-249` | 正确性 | `DeleteAllSMS` 只清 "ME"，`ListSMS` 同时读 "SM" 与 "ME"，清空后 SIM 消息残留重现 | 对称遍历两个存储 |
| M4 | `at_backend.go:188-231`、`business_adapter.go:81-96` | 错误处理 | `GetServingSystem`/`GetSignalInfo` 子查询错误全吞，断连时返回零值 + nil，"没信号"与"设备坏了"无法区分 | 首个关键查询错误向上传播 |
| M5 | `qmi_backend_ussd.go:237-299` | 设计/副作用 | USSD 前置流程 `PowerCycle` 改域偏好，会话结束后从不恢复，一次 USSD 永久改写模组域选择 | 会话结束后恢复原偏好 |
| M6 | `qmi_backend_ussd.go:59-64,134-143,382-397` | 并发 | `ussdMu` 只锁 ExecuteUSSD，CancelUSSD 不加锁；结果投递非阻塞满则丢，迟到的旧会话回调被误判为新会话结果 | 会话加代际序号；Cancel 共享锁 |
| M7 | `factory.go:19-30` | 设计 | `NormalizeBackendMode` 把一切未知值（含 `"auto"`）静默归一化为 AT，拼错模式无感知落到 AT 后端 | 未知值报错，auto 显式处理 |
| M8 | `internal/modem/manager.go:1502-1530,1372-1433` | 并发 | `+CMTI` 每短信起 goroutine 做存储切换，CPMS 全局状态被并发踩（与 3.2 H2 同源） | 流程级互斥 + URC 派生并发限流 |
| M9 | `business_adapter.go:64-79` | 错误处理 | `Identity` 对 IMEI 外全部查询静默吞错，SIM 拔出被掩盖，与 IMEI 失败即报错语义不一致 | 吞错时记录 |

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `command_backend.go:589-596` | 正确性 | 注册正则要求两个数字字段，单字段应答 `+CREG: 1` 判未注册（modem 包已正确支持） | 接受单字段形态 |
| L2 | `internal/modem/manager.go:1656` | 性能 | `executeAT` 每次 `time.After(5s)`（与 3.2 L6 同源） | 复用 Timer |
| L3 | `at_backend.go:354-357`、`qmi_backend.go:414-431`、`command_backend.go:107,516,578,590`、`at_factory.go:64` | 错误处理/卫生 | `CancelUSSD` 吞错恒返回 nil；QMI `operatorRetried` 恒真分支 + 整段注释死代码；`CommandBackend.Radio` 热路径每次 `regexp.MustCompile`；`WaitReady(15s)` 结果被忽略 | 分别修复 |

**模块整体评价**：接口分层（`DeviceBackend`/`ModemBackend`/`BusinessAdapter` + Port 接口 + 编译期断言）清晰、依赖方向正确，QMI USSD 状态机与 MBIM 封装是写得最仔细的部分。但后端对 modem.Manager 几乎裸委托，SMS 链路在 AT/QMI 路径整体不可用（H1-H3），异常设备输入可放大为资源耗尽（H4）。

---

### 3.4 application（internal/application）

#### High

**H1. 长短信跨轮次重组永久丢失**
- `internal/application/sms/service.go:338-356`（`Reassemble`，调用点 168） | 正确性
- 每次 `Refresh` 新建 `smscodec.NewReassembler()`，轮询间无状态。多段短信若分两次轮询到达，首轮分片被剔除、次轮新重组器里没有 p1 又被剔除 → 完整短信永远不会进入 cache/存储/通知。`Cleanup(24h)` 对新建实例无效。QMI/MBIM/串口 command backend 与应用重启后模块残留分片都走这条无状态路径，必现丢失。
- 建议：`Reassembler` 提升为 Service 持久字段（互斥保护），配 TTL 清理。

**H2. vowifi 订阅永不注销 + 事件驱动无界并发恢复**
- `internal/application/vowifi/service.go:127-150` | 并发/资源
- `Subscribe(32)` 的 unsubscribe 被丢弃，`followRuntime` 永不退出（App.Stop 不停止它）；每个 `sim.updated`/`network.updated`/`backend.modem.reset` 事件都 `go Recover(context.Background())`，网络状态抖动时恢复无界并发、互不串行，与用户 Enable/Disable 竞态；错误被 `_ =` 吞掉。
- 建议：持有 unsubscribe 挂 ctx 生命周期；Recover 单飞 + 可取消 ctx；记录失败日志。

#### Medium

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| M1 | `internal/runtime/events.go:31-37` + `notification/service.go:98` | 并发/正确性 | `EventBus.Publish` 对慢订阅者 drop；notification 消费者同步调 Swift Sink，桥接慢时 `call.ended`/`sms.received` 被丢弃，来电卡片永不消失、漏通知 | 通知消费者与 Sink 解耦；恢复后对账 |
| M2 | `extras/service.go:335-342`、`app/app.go:252-261` | 正确性/安全 | `reject_call` 强制校验 call_id 但 `extras.Reject` 完全忽略它直接 `AT+CHUP`：3 秒轮询窗口内可能挂断**新**呼叫 | Reject 接收 call_id 比对 `s.active.ID`，不一致返回 OperationConflict |
| M3 | `operation/manager.go:46-48,59-75` | 资源 | operation items 永久驻留，长跑应用内存线性增长（与 3.6 条目交叉确认） | 终态保留最近 N 条 |
| M4 | `notification/service.go:154-163` + `runtime/runtime.go:253-257` | 正确性 | 离线 transition 同时发布 `device.status.changed` 与 `device.offline`，notification 对两者都计数，阈值 5 实际 3 次触发 | 仅对 `EventDeviceOffline` 计数 |

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `firmware/service.go:812-813` | 性能 | `progressWriter.Write` 内层循环每次编译正则 | 提升为包级 var |
| L2 | `sms/service.go:50-52,66-68,190-230` | 正确性 | 重启后模块中超过 500 条上限的旧消息被当 fresh 重新发通知 | 启动首轮 Refresh 只建基线不发布 |
| L3 | `network/service.go:243-270` | 正确性 | `publishRadioState` 部分失败仍发布空状态，瞬时 AT 失败让菜单栏错误显示离线 | 任一来源失败时跳过发布 |
| L4 | `network/service.go:82-93,95-114` | 性能/资源 | 每秒一次 SQLite 事务 + 发布事件，数值未变也发 | 降频或去重 |
| L5 | `sms/service.go:266` | 设计 | `recordSent` 用负纳秒时间戳做 Index，`CommandBackend.ReadSMS` 显式拒绝负索引，跨层语义不一致 | 独立字段区分本地记录 |
| L6 | `firmware/service.go:746,773` | 正确性 | `strings.Fields(command)[0]` 对纯空白命令 index out of range panic | 防御判断 Fields 长度 |
| L7 | `esim/service.go:112-122` | 设计/一致性 | `esim.Rename` 不经过资源锁与操作跟踪，与其他 eSIM 操作并发语义不一致 | 统一走 `ops.Start` + `Acquire(ResourceSIM)` |
| L8 | `operation/manager.go:77-115` | 错误处理 | `operation.run` 无 panic recover，后台任务 panic 直接终止进程（含 UI） | run 内 defer recover 转 Failed |
| L9 | `device/service.go:75-80` | 错误处理 | 双错误时后一个被 `&& out.Snapshot.LastError == ""` 条件吞掉 | `errors.Join` 保留全部失败 |
| L10 | `cmd/djonehub/main.go:31-34,43` | 安全 | `Authenticator` 为 nil 时 `-listen 0.0.0.0:7575` 即无凭据开放全部设备控制（与 2.1 同源） | 非 localhost 绑定默认要求 token |
| L11 | `vowifi/service.go:135-149`、`vowifihost/host.go:192-200,224-226` | 正确性 | 每个事件 spawn 一个 `Recover` goroutine（与 H2 同源，见 3.6 H2 的端口泄漏面） | 单飞 + 串行化 |

**模块整体评价**：用例层组织清晰，capability 门控与 ResourceAT/SIM/Network 仲裁贯穿一致，事件契约（notification 包）以 testdata 冻结。主要短板是状态生命周期管理（长短信重组无跨轮状态、vowifi 订阅/恢复不收敛、operation items 无上限——"运行越久越糟"）与通知路径可靠性（事件丢失无补偿、reject 校验与执行脱节、离线阈值双计数）。AT 命令在 modem.Manager 层已有串行化兜底，应用层锁缺失未造成线路级破坏，但存在逻辑交错风险。

---

### 3.5 platform（internal/platform）

> 本模块未发现 Critical/High，未发现 shell 注入、路径穿越或明显数据竞争。

#### Medium

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| M1 | `darwin/adapter.go:109-136` | 性能/设计 | macOS 探测失败端口无冷却，每轮 2s 轮询重放完整 2s 超时；系统上存在任何一个不应答 AT 的串口设备就拖慢整个 runtime 循环（Linux 有 `probeFail` 10 分钟冷却，darwin 没有） | 移植 Linux 冷却机制 |
| M2 | `darwin/network.go:205-243` | 正确性 | 网络接口名按 `candidate.StableID` 永久缓存、命中只查存在性：模块重插后旧 `enN` 可能已被系统分配给别的网卡，永久上报错误接口 | 缓存加 TTL + 命中时 USB 身份复核 |
| M3 | `linux/adapter.go:24-27,209-241` | 设计/性能 | 头注释声称"discovery 阶段不打开设备"，但 `selectATPort` 对每个候选逐一 `serial.Open` 探测（单候选 2s、全扫描 25s 预算），不可被 ctx 中断，且在 runtime 循环内同步执行，最长阻塞 25s | 探测移入 `Backends.Open`；预算降级；探测接受 ctx |
| M4 | `internal/modem/force_release_linux.go:39-61` | 安全 | 对 fuser 输出所有 PID 无条件 SIGTERM，不校验 cmdline/属主（root 运行时可能杀任意服务）；fuser→kill 间 PID 复用 TOCTOU；固定 200ms 等待无确认无 SIGKILL 升级 | kill 前查 `/proc/<pid>/cmdline`；kill 后 poll 退出；超时升级 SIGKILL + 告警 |
| M5 | `darwin/adapter.go:109-137` | 正确性/设计 | tty 探测全部失败即 `return nil, nil`，放弃可用的 USB-raw 传输路径（libusb `OpenAT` 完全可用）；USB-only 分支只在 `len(ports)==0` 触发，二者不互补 | tty 失败时回退返回 USB-only 候选 |
| M6 | `linux/adapter.go:63-65` + `runtime/runtime.go:120` | 架构 | Linux `Discover` 对所有候选做探测但 runtime 只用 `candidates[0]`，其余探测全部浪费；darwin 探测到第一个应答即返回，平台间契约不对称 | runtime 显式声明单设备约束，平台层只探测被使用的候选 |

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `linux/adapter.go:140` | 正确性 | `filepath.Glob(":1.*")` 硬编码接口模式，部分 EC25 部署 AT 口在 `:2.0`/`:3.0` 会被静默漏掉 | 枚举 `:*` 全部接口再分类 |
| L2 | `darwin/adapter.go:146-154` | 错误处理 | eSIM 端口创建错误静默吞掉，无日志无诊断 | 至少 `log.Printf` |
| L3 | `darwin/native/bridge_darwin.go:51-55` | 正确性 | `handleEvent` 不校验 `activeBridge`，UI 退出后仍向 Swift 发事件（回调有门控，此处没有） | 与回调一致加门控 |
| L4 | `darwin/native/bridge_darwin.go:78-90` | 并发 | `select+default+close` 非原子，两个并发回调可双重 close panic（当前 Swift 单次触发，风险极低） | `sync.Once` |
| L5 | `darwin/usb_at_darwin.go:296-302` | 并发 | `drainLocked` 在持续 URC 流下无限循环，长期占用 `u.mu`，阻塞所有命令 | 读取次数/时长上限 |
| L6 | `startup/startup_darwin.go:65-76` | 设计 | plist 硬编码 `-listen`/`-web-dir`，与 main.go flag 默认值双处维护；`Status().Enabled` 仅凭 plist 存在判定 | 由 `New()` 接收当前值写入；用 launchctl 判定 |
| L7 | `internal/modem/imei_probe.go:19-52` | 正确性/资源 | IMEI 缓存以端口路径为键：路径复用 10 分钟内身份错配；死路径条目永不清除 | 键加 USB 身份；惰性清理 |
| L8 | `internal/modem/imei_probe.go:78-107` | 错误处理 | `SetReadTimeout` 错误被忽略；非 timeout 读错误（端口拔掉）也继续空转 | 非 timeout 错误直接返回 |
| L9 | `darwin/adapter.go:46,242`、`network.go:254,428` | 性能 | 热路径每次调用 `regexp.MustCompile` | 包级 var |
| L10 | `linux/tunnel_linux.go:45-46` | 正确性 | `tunDevice.Read/Write` 无 nil 保护（Close/Name 有），并发 Read/Close 可 panic | 加 nil 判断或 atomic 指针 |
| L11 | `darwin/adapter.go:123-129` | 正确性 | tty 探测成功后直接取 `usb[0]` 配对，不验证同设备，多模组下身份可能错配 | 按 IOKit registry/locationID 配对 |
| L12 | `darwin/native/bridge_darwin.go:6-15` | 架构 | 直接链接 `.build/release/libDJOneHubNotifier.a` 预编译产物，无 ABI 版本握手，两侧不同步是运行时崩溃而非构建期报错 | config JSON 携带 ABI 版本校验 |

**模块整体评价**：构建标签组织清晰，共享逻辑没有泄漏到平台代码；所有 exec 均为固定参数无 shell 拼接，无 sudo/kext 权限提升，临时文件与 libusb/TUN 句柄生命周期管理严谨，桥接层与 Swift 侧契约吻合。主要短板在"探测与轮询"主路径：25s 扫描预算与失败端口无冷却使 Discover 成为 runtime 循环上的不稳定阻塞点，端口强制的 SIGTERM 缺少归属校验，接口名缓存缺少 TTL。

---

### 3.6 api/storage/config/app/runtime 等小模块

#### Critical

**C1. 本地 HTTP API 完全无鉴权，且无 CSRF/Origin/Host 校验**
- `internal/app/app.go:211-240`、`internal/api/http/server.go:121-127,628,1037-1082`、`cmd/djonehub/main.go:43,77`、`internal/config/config.go:251-252` | 安全
- 完整影响链：任意网页可静默读取实时短信正文/通话记录/IMEI-ICCID（WS 事件流未脱敏）；用用户 SIM 发短信；删除 eSIM profile；`/device/actions/raw-at` 执行任意 AT 命令；`firmware/actions/adb/unlock` 解锁 ADB 并开交互式 shell；固件备份写到任意路径（`firmware/service.go:1046-1060` 的 `resolveOutputPath` 可运行用户选定目录脚本）。
- 建议：接入 web 凭据或随机 token 做鉴权；WS 与全部 POST 校验 Origin/Host；拒绝非 JSON 简单请求或校验 `Sec-Fetch-Site`；`openapi.go` 声明 securityScheme 与实际对齐。（详见 2.1 汇总）

#### High

**H1. vowifihost.Enable 失败路径泄漏 modem 端口与 context**
- `internal/vowifihost/host.go:67-114,202-208,138-156,160-200,210-229` | 资源/并发
- `Enable` 在 `factory.Open` 之前就保存 `h.cancel`；`fail()` 既不 cancel 旧 child context 也不关闭已打开端口，反复失败累积打开的 modem 端口与事件消费者。`followRuntime`/`consumeEvents` 每个事件 spawn `Recover` goroutine，多个 Reconnect 对同一端口并发操作；恢复路径不持 `ResourceVoWiFi` 锁，与用户操作同时发指令。
- 建议：`fail()` 统一清理 cancel/port；单 goroutine 状态机串行化 Enable/Disable/Reconnect/Recover；恢复去抖合并。

**H2. events WebSocket 无读循环、无读写 deadline**
- `internal/api/http/server.go:1055-1107,1061-1067` | 资源/正确性
- 从不读连接：客户端不再读但 TCP 保持打开时 `writeTextFrame` 无限阻塞，handler 与 32 槽订阅永久泄漏；无 pong/keepalive，fd 挂到下一个事件才回收。初始 snapshot 在 `Subscribe` 之前写出（丢事件窗口），snapshot ID 复用 `LastID()`（客户端按 ID 去重会漏事件）。
- 建议：gorilla/websocket（读循环 + SetReadDeadline + ping-pong + SetWriteDeadline）；先订阅再发 snapshot 补差；snapshot ID 后置语义。

#### Medium

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| M1 | `operation/manager.go:59-75,144-152`、`server.go:68-118` | 资源/设计 | operation items 无 TTL 无上限；`Cancel(id)` 已实现但路由表无取消端点；卡死的操作（WithoutCancel 脱离请求）永久占锁，之后所有 SIM 操作一律 409 | 增加 `DELETE /api/v1/operations/{id}`；终态 TTL 清理 |
| M2 | `openapi.go:36,50-64,80-108`、`server.go:475-509,987-1011` | 设计/错误处理 | 文档声明 GET-only 但服务端支持 GET+PUT；模板声明 401 而服务端永不返回；async 只声明 202/400/422/503 实际还有 401/404/409/504 | 补齐 put 与 409/504；鉴权落地后对齐 security scheme |
| M3 | `extras/service.go:145-153,335-341`、`network/service.go:425-443`、`runtime/runtime.go:291-298,270-289` | 并发/架构 | 部分 AT 入口（通话轮询、network diagnostics）绕过 ResourceAT 锁；firmware "读 usbcfg→写 usbcfg" 复合序列按命令加锁可被插入；`Backend()` 在 RLock 下返回指针后可能被并发关闭 | 会话级租约；Backend 引用计数或在锁内使用 |
| M4 | `app/app.go:267-290` | 资源 | `Stop()` 不等待轮询器就 `Store.Close()`，在途写库报 "database is closed" 被静默吞掉（与 2.5 同源） | cancel + WaitGroup 后再关库 |
| M5 | `config/manager.go`、`persist.go:11-133`、`config.go:173-234,251-252` | 设计/安全 | 遗留死代码与双份实现并存；`GetConfig`/`GetDeviceByID` 返回共享指针；凭据明文 YAML、目录 0o755（与 2.6 同源） | 删除未引用函数；目录 0700；文档说明 keyring 方案 |

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `server.go:475-509,421-440,1258-1277`、`extras/service.go:369-371` | 错误处理 | `SaveNote` 缺 iccid/超长返回裸错误 → 500；`OperationCancelled` 落入 default 返回 500 | 服务层返回 derrors 分类；显式状态码映射 |
| L2 | `storage/sqlite.go:118-132,306-342` | 正确性/性能 | 短信去重 UNIQUE 不含 iccid，双卡同时收到相同短信后到者被 IGNORE 丢弃；ListSMS 全表扫描无分页 | UNIQUE 加 iccid（迁移 v3）；LIMIT/OFFSET |
| L3 | `network/service.go:82-93,113`、`firmware/service.go:182-261` | 性能 | traffic 每秒无条件发事件（无变化也发）；firmwareStatus 每次 4 条 AT + adb 探测 | 数值去重；status 短 TTL 缓存 |
| L4 | `runtime/runtime.go:93-95,111-206`、`server.go:1088-1107,1242-1256`、`firmware/service.go:803-820` | 并发/正确性/性能 | HTTP Rescan 与轮询 loop 并发 scan 可能把已关闭 backend 挂回 `r.backend`；`writeTextFrame` 对 >65535 字节事件直接丢弃不告警；`publicText` 脱敏规则脆弱；`progressWriter.Write` 每行编译正则 | 串行化 scan；超大事件截断+标记；显式字段白名单；正则提升包级 |

**模块整体评价**：分层清晰（domain/application/runtime 职责明确）、`EventBus.Publish` 非阻塞、`Subscribe` 关闭语义无竞态。但"本地服务 = 安全边界"的假设完全不成立（C1），是必须立即修复的面；vowifihost 与 WS 的资源泄漏、operation 无取消机制其次；OpenAPI 与实现脱节、config 遗留建议随修复清理。

---

### 3.7 pkg（mbim/smscodec/logger）

#### High

**H1. 四个解析器信任设备可控 count 并先行预分配，可致 OOM/panic**
- `pkg/mbim/sms.go:55-56`、`providers.go:25-29`、`databuffer.go:35`（经 subscriber.go:32 可达）、`uicc_fileaccess.go:122-127` | 安全/资源
- `make([]SMSRecord, 0, count)` 等：count 来自 InfoBuffer 且未校验与剩余缓冲区的关系。count=0xFFFFFFFF 时申请约 128GB 后备数组（64 位虚拟分配 + GC 停顿，受限环境 OOM panic；32 位 makeslice cap 溢出硬崩溃）。恶意模组或本地 unix socket 代理可触发。
- 建议：预分配前校验 `count <= (len(buf)-header)/elemSize`，或循环内 append 靠 u32At 边界检查终止。

**H2. fragment collector 无上限与清理，可耗尽内存**
- `pkg/mbim/fragment.go:27-58`、`device.go:168-182,202-211,125-134,245-254` | 资源/安全
- total/current 全来自线上数据：首分片声明 total=0xFFFFFFF0，后续每分片 append 最多 64KB 永不完成；乱序/畸形/丢失分片或 ctx 超时后 collector 永久滞留在 map 中（ctx 超时只 removePending 不 removeCollector），唯一清理点是断线 failPending。
- 建议：限制单 collector 累计字节与分片数；ctx 超时同步移除；周期清理未完成 collector。

#### Medium

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| M1 | `smscodec/reassembler.go:36-41` | 正确性 | 重组缓存 key 为 `sender+"_"+ref` 不含 Total：8 位 ref 回绕或同 sender 同 ref 不同 Total 时分片互相污染，永远凑不齐或错误拼接 | key 加 Total，Add 时校验一致性 |
| M2 | `mbim/subscriber.go:16-40`、`databuffer.go:30-44` | 正确性 | SUBSCRIBER_READY_STATUS 布局假设与规范不一致（固定部 28 vs 规范 36 字节，count 读取位置错误）：非空数组时解出乱码 MSISDN；测试夹具编码了同样假设无法暴露 | 对照规范修正 fixed=36、count@28/offset@32；补非空数组测试向量 |
| M3 | `mbim/device.go:245-254`、`snapshot.go:44-61` | 并发/资源 | readLoop 退出时从不 close `d.indications`，`Monitor.Run` 永久阻塞在 `<-Indications()`，每次重连泄漏一个 goroutine | 收尾路径 close；Monitor 已有 `!ok` 分支自然退出 |

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `mbim/device.go:153-164` | 错误处理 | FUNCTION_ERROR/HOST_ERROR 消息被静默丢弃，对应 Command 挂起到调用方 ctx 超时（可数十秒）且错误码丢失 | 增加处理并派发给 pending |
| L2 | `mbim/ipconfig.go:50-57,70-77` | 资源/性能 | DNS 循环以线上 dnsCount 为界无上界校验，恶意 count 让调用 goroutine 空转数十亿次快速失败迭代 | 循环前校验 `off+count*elemSize <= len` |
| L3 | `mbim/device.go:202-211`、`fragment.go:12-21` | 设计/正确性 | COMMAND_DONE 与 INDICATE_STATUS 分片共用同一 collector map（key 仅 tx），设备对某 tx 双消息时重组互相掺入 | key 加方向 |
| L4 | `logger/logger.go:33-57,138-146` | 设计 | `readerIMSIRegistry` 全仓库无写入者，LookupIMSIByReader 恒 false，相关分支是死代码 | 补写入 API 或删除 |
| L5 | `mbim/fragment.go:64,80`、`smscodec/wbxml_omacp.go:277,368,496` | 正确性 | 线上数据转 int 参与边界判断，32 位平台溢出为负值时绕过检查，`make([]byte, n)` 负容量 panic（当前目标平台不受影响） | uint64 边界运算 |

**模块整体评价**：MBIM 编解码结构清晰（infoReader 统一边界检查、uint64 加法防溢出），SMS 侧对 AT 头长度与 GSM7 尾比特做了主动修正，错误均以 error 传播。主要风险集中在"信任线上计数/长度字段"一类惯用模式：预分配、分片累积、重组缓存均缺与缓冲区成比例的校验与上限；SUBSCRIBER_READY_STATUS 布局与规范不一致属隐蔽正确性缺陷。

---

### 3.8 web 前端（web/）

#### High

**H1. 本地 API 无鉴权 + WS 无 Origin 校验（前端暴露面）**
- `web/src/stores/device.ts:112`、`web/src/views/FirmwareView.vue:292`；后端佐证 `server.go:628,1037-1088,1109-1123`、`app.go:211` | 安全
- 应用对浏览器开放的本地 API 无任何身份验证，WS 端点不校验 Origin：恶意网页可打开 `ws://127.0.0.1:7575/api/v1/events/ws` 静默读取全部设备数据，或在 ADB 解锁时对模组 shell 输入任意命令；`text/plain` 简单请求可触发全部写操作（含 `raw-at`）。前端 i18n 已有 `errors.unauthenticated` 文案与后端错误路径，但未启用。（与 2.1 同源）
- 建议：WS 升级校验 Origin/Host；写请求携带 CSRF token 或会话 token（fetch header + WS subprotocol）。

#### Medium

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| M1 | `web/package.json:15-22` | 依赖 | 直接 import `@ant-design/icons-vue` 但未声明在 dependencies，仅靠传递依赖提升（7.0.1）；依赖策略变化即构建失败 | 显式声明；CI 加 `npm audit`（本次未发现已知高危版本：vue 3.5.40/vite 7.3.6/antd 4.2.6） |
| M2 | `services/api.ts:35-49`、`App.vue:1081-1112` | 正确性 | fetch 无超时/AbortSignal：后端操作挂起时 Promise 永不 settle，`viewLoadInFlight` 永久持有，该视图**静默永久停更**，只能整页刷新 | 30s AbortController；in-flight 超时视为失效可替换 |
| M3 | `App.vue:877,888,899` | 正确性 | 用 `error.message.includes('cancelled')` 判断用户取消文件选择，后端改文案/本地化后每次取消弹错误 | 匹配 `APIError.code` |
| M4 | `App.vue`（约 1365 行）、`views/context.ts:6` | 设计/架构 | App.vue 上帝组件持有全部视图状态，`ViewContext = Record<string, any>` 无类型约束；状态分散在根组件难以测试 | 显式 TS 接口；按域拆独立 Pinia store |

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `stores/device.ts:11-12,118` | 并发/内存 | operations map 永久增长；重连固定 2.5s 无退避无上限 | 终态超时删除；指数退避+抖动 |
| L2 | `main.ts:3,10` | 性能 | `app.use(Antd)` 全量注册组件（图标已按需，组件未按需） | 按需注册或 AntDesignVueResolver |
| L3 | `App.vue:1182` | 正确性/i18n | runVowifi 复用 `esim.operationAccepted` 命名空间 | vowifi 命名空间独立键 |
| L4 | `index.html:2`、`components/OperationStatus.vue:34`、`views/CallsView.vue:17-19` | 设计 | `<html lang="en">` 硬编码（zh 用户屏幕阅读器错误）；操作状态/未知通话状态渲染原始 key | watch locale 同步 lang；i18n 映射回退 |
| L5 | `components/ErrorState.vue` | 设计 | 死代码（0 处引用）；`SmsView.vue:85-108` 会话列表无虚拟化 | 删除或复用；超阈值懒加载 |

**模块整体评价**：组织良好、工程化程度高的 Vue 3 管理前端：API 服务层、Pinia store、i18n（中英双全）、WS 事件去重/乱序/缺口重同步设计逻辑自洽，无 v-html/innerHTML、无凭据前端存储、敏感标识默认掩码。主要风险集中在本地控制面缺验证与 Origin 校验（本质是后端缺口，但直接决定前端安全边界）、根组件过重、API 无超时"永久卡死"隐患与 icons 依赖未声明。

---

### 3.9 macOS Swift 原生 UI（macos/DJOneHubNotifier）

#### High

**H1. Swift 6 `@MainActor` 通知代理回调可导致进程直接崩溃**
- `Sources/DJOneHubNotifier/NativeUIHost.swift:121`（`UIAppDelegate`）、`377-401`（`willPresent`/`didReceive`） | 正确性/并发
- 包以 `swift-tools-version: 6.0` 编译，`UNUserNotificationCenter` 通过 ObjC 协议回调；Swift 6 对 actor-isolated 协议方法插入动态隔离检查，回调在非主线程到达（应用由通知点击冷启动时 `didReceive` 已知会在后台队列）即 main-actor 隔离违规直接 abort；且系统要求 `completionHandler` 必须在回调内调用，无法用常规 hop 兜底。
- 建议：两个代理方法改 `nonisolated`，内部 `Task { @MainActor in ... }` 访问状态并同步调用 `completionHandler`。

#### Medium

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| M1 | `NativeNotificationService.swift:177-186`、`PanelContent.swift:35-43`、`NotifierView.swift:97-127` | 安全 | SMS 正文（含验证码）明文进入横幅/锁屏/通知中心，应用退出后历史仍保留，无脱敏/关闭选项 | 提供"仅显示 sender"偏好或文档明确 |
| M2 | `internal/platform/darwin/native/bridge.go:295-299` | 并发/错误处理 | Go→Swift 命令通道满时 `select default` 静默丢包：`reject_call`/`open_dashboard` 是用户点击关键命令，丢失后无任何反馈 | 丢弃时回传 `command.dropped` 或阻塞+超时 |
| M3 | `NativeUIHost.swift:306-326`、`NotifierView.swift:44-49` | 正确性 | `rejectingCallID` 无超时：命令丢失或设备无响应时 UI 永久卡"拒接中"且按钮禁用，用户无法重试 | 5-10s 超时恢复按钮；callRejectFailed 未匹配也清状态 |
| M4 | `NativeUIHost.swift:69-76,104-114`、`NativeNotificationService.swift:225-235` | 错误处理 | 事件解析失败静默丢弃、编码失败静默 return、C ABI 无错误码，来电/短信核心通知丢失无法排查 | 至少记录解析失败（含类型）；增加 `ui.error` 回传 |
| M5 | `NativeUIHost.swift:225-239,197-210` | 正确性 | 面板单槽：通话中收到短信直接覆盖来电面板（拒接按钮消失）；`callEnded` 无条件 `panel.hide()` 连带隐藏 SMS/错误面板，custom 模式下短信内容永久丢失 | 面板按类型分槽/栈式；callEnded 只关通话内容 |

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `BridgeModels.swift:165-183,189-200,202-215` | 性能 | 每事件三重 JSON 解析；每次日期解码新建两个 `ISO8601DateFormatter`（10.13+ 线程安全，可缓存） | 缓存 formatter；避免重编码 |
| L2 | `NativeUIHost.swift:23` | 设计 | `MainActor.assumeIsolated` 依赖 Go 侧 LockOSThread 契约，未来调用方破坏即 trap 且无诊断 | 显式线程检查 + 可读错误；契约写入 bridge.h |
| L3 | `NativeUIHost.swift:430-451,423-428` | 安全 | 菜单栏 tooltip/无障碍标签暴露运营商名与制式（低敏感，确认是产品决策即可） | 确认或隐藏 |
| L4 | `NativeNotificationService.swift:147-161` | 设计 | 来电通知未设置 `interruptionLevel`，Focus 模式下来电横幅被静默拦截 | 来电用 `.timeSensitive` |
| L5 | `NativeUIHost.swift:121-166` | 设计 | 未实现 `applicationSupportsSecureRestorableState`（macOS 12+ 安全警告） | 返回 true |
| L6 | `NativeUIHost.swift:125,142-144` | 设计 | `webURL` 解析后从未使用（死代码） | 删除或注释说明 |

**模块整体评价**：C ABI 契约完整（线程归属、回调线程、内存生命周期文档化且实现一致——字符串全部即时拷贝、回调同步返回、无悬垂指针/use-after-free）；`@MainActor` 隔离、事件缓冲、权限状态回传、无 bundle 降级路径处理得当；未发现主线程死锁、循环引用或未释放 Timer。主要短板是"静默失败"文化：命令丢弃、事件解析失败、拒接状态卡死均无错误通道，以及面板单槽设计导致通话期间内容互相覆盖。

---

### 3.10 入口与跨模块架构（cmd、app、scripts、go.mod）

#### High

**H1. `Authenticator` 已设计但从未接线（见 2.1）**
- `internal/api/http/server.go:628`、`server.go:121-127`、`internal/app/app.go:211-240`、`cmd/djonehub/main.go:77` | 安全
- 完整链路见 2.1。另注意 `-listen` 无 loopback 约束。

#### Medium

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| M1 | `pkg/logger/logger.go:17`、`vohive-open/cmd/vohive/main.go:46` | 正确性 | 新入口从未调用 `logger.Setup`，全二进制 zap 日志保持 Nop 静默丢弃（设备层 modem/backend/esim 全部受影响） | main() 调用 Setup 或统一标准库 log |
| M2 | `internal/app/app.go:286-290`、`application/*`、`operation/manager.go:68` | 生命周期 | `Stop` 不停 SMS/Network/Extras 轮询器；`context.WithoutCancel` 使在途 operation 不可取消（详见 2.5） | 可等待 Stop + `Shutdown(ctx)` |
| M3 | `cmd/djonehub/main.go:51-64`、`bridge.go:269-275` | 生命周期 | HasUI 分支双路并发 shutdown（UI 退出 + ctx.Done 各跑一遍 `NativeUI.Stop()`+`shutdown()`）；`Bridge.send` 对 started/exited 无检查，可能向已停止的 AppKit 循环投递事件；`server.Shutdown(context.Background())` 无超时 | 单一 shutdown 路径；send 丢弃；超时 ctx |
| M4 | `internal/config/config.go:229-279`、go.mod:29 | 设计/依赖 | viper 全家桶随死代码编译进产物；全局 viper 实例测试共享污染；`file-rotatelogs v2.4.0+incompatible`（2019）、`google/uuid v1.3.0` 旧依赖经死路径引入（详见 2.6） | 类型下沉、删 Load、移除 viper |

#### Low

| # | 位置 | 类别 | 问题 | 建议 |
|---|------|------|------|------|
| L1 | `scripts/build-macos.sh:8-9,47-54,58,174` | 安全/设计 | `VERSION` 未校验直接进 PlistBuddy 与 PACKAGE_NAME（`/` 可写出 dist 外）；`BUILD_ROOT="${TMPDIR:-/tmp}"` 直接 `rm -rf` 未用 `mktemp -d`；版本硬编码 v0.1.5-preview；`swift build --disable-sandbox`（libusb SHA-256 校验、`.tmp` 原子下载、`set -eu` 做得好） | 校验 VERSION 格式；mktemp；版本必传 |
| L2 | `server.go:1037-1082,1084-1086` | 安全/正确性 | events/ws 手写升级不校验 `Sec-WebSocket-Version: 13`、不校验 method；升级后从不读帧，半开连接永久泄漏 handler 与订阅 | gorilla/websocket + GET + version 13 + 周期读帧 |
| L3 | `internal/runtime/locks.go:41-48` | 正确性 | `Acquire` 在锁可用与 ctx 取消同时满足时随机返回假冲突；release 不幂等，重复调用永久阻塞（当前调用方均 defer 单次释放） | 优先判空；文档化单次释放 |
| L4 | `internal/app/app.go:40-49` 与 `darwin/adapter.go:145-153` | 设计 | eSIM AT 端口组合逻辑分两处维护；`internal/api/http` 直接 import `internal/platform/startup` 穿透分层 | 构建收敛到 backend 层；startup 经 app 注入 |
| L5 | go.mod:5-21、third_party/ | 依赖 | 10 条 replace 中 multierr/pkg-errors 与上游逐字节相同；fork 无自动同步；根目录与 vohive-open 声明相同 module path，`go build ./...` 静默跳过旧树 | 删无差异 replace；记录 fork 补丁；vohive-open 独立 path |
| L6 | `pkg/logger/logger.go:33-38`、`esim/pki/pki.go:5-6,53-60` | 设计/依赖 | IMSI 注册表死状态；`go:generate` curl 无校验和，init 失败仅向 Nop logger 记录 | 删除/校验和 |
| L7 | `cmd/djonehub/main.go:44-47,57-61` | 生命周期 | `ListenAndServe` 失败在 goroutine 内 `log.Fatal` 直接 os.Exit 跳过清理，NativeUI 仍启动指向连不上的 URL | 预监听端口再启动 UI；错误回传主流程 |

**整体架构评价**：分层总体健康——domain 是纯净叶子层，runtime/backend 不反向依赖 application，单设备状态机、资源锁与事件总线设计清晰且有测试；main/app 装配把平台差异收敛在 `app.New` 的 switch 中，native UI 与 HTTP 的线程模型（LockOSThread + 主线程跑 AppKit）正确。主要风险：HTTP 控制面设计了 `Authenticator` 却从未接线（实际可利用的本地攻击面）；关停路径不完整（轮询器不可停、在途 operation 不可取消、双路并发 shutdown、SQLite 先于轮询器关闭）；迁移残留大量死代码面（viper 配置加载、zap 初始化遗漏、旧通知配置）。

---

## 4. 修复优先级建议

### 第一批（安全与数据丢失，建议立即修复）

1. **接入 API 鉴权 + Origin/CSRF 防护**（P0）：`app.go` 注入 Authenticator；WS 校验 Origin/Host；写端点校验 Content-Type/Sec-Fetch-Site。
2. **SMS 链路修复**（P1）：注册 SMS 回调消费者；`SMSListAllPDU` 返回真实索引；后端 ReadSMS 内 PDU 解码；`Reassembler` 提升为 Service 持久字段；存储切换流程级互斥。
3. **AT 应答正确性**（P1）：超时后 drain；`>` 提示符精确匹配；URC 判定前置。
4. **eSIM 同步原语**（P1）：`opDone` 持锁替换 + atomic；watchdog 与在途 APDU 解耦。
5. **协议解析上限**（P1）：mbim 预分配 count 校验；fragment collector 上限与清理；modem lineBuf 上限。
6. **macOS `@MainActor` 回调**（P1）：代理方法改 nonisolated。

### 第二批（可靠性与生命周期）

7. 事件/通知链路：非阻塞发送 + 丢弃计数；notification 与 Sink 解耦；Swift 拒接超时。
8. 关停顺序：轮询器可等待 Stop；operation `Shutdown(ctx)`；单一 shutdown 路径；WS 升级改 gorilla/websocket。
9. vowifihost 状态机收敛 + 恢复单飞。
10. `logger.Setup` 接线（恢复设备层日志）。

### 第三批（架构清理）

11. 删除死代码面（viper config、IMSI 注册表、`vohive-open` 独立 module path、无差异 replace）。
12. `App.vue` 状态按域拆分 + `ViewContext` 类型化。
13. 敏感信息脱敏策略统一（日志、通知、publicText 白名单）。
14. 跨路径一致性（EF_IMSI 首位、AT 端口接入仲裁器、两套 AT APDU 封装合并、COPS 副作用读）。
