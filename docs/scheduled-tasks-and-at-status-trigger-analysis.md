# 定时任务与 AT 状态误触发分析

本文记录 DJOneHub 当前生产代码中的周期任务、条件定时器和前端刷新任务，并说明稳定设备状态被记录为 `URC: 注册状态变更`、`URC: SIM 插拔` 和 `URC: SIM 状态` 的触发链。

## 1. 后端常驻周期任务

| 任务 | 周期 | 首次执行 | 主要行为 | 设备访问影响 | 实现位置 |
| --- | ---: | ---: | --- | --- | --- |
| 设备发现 | 2 秒 | 启动后立即 | 枚举设备、检测断开、创建或恢复后端 | Windows 每轮枚举 COM；成功 IMEI 探测缓存 10 分钟，失败端口冷却 10 分钟 | `internal/runtime/runtime.go`、`internal/platform/windows/adapter.go` |
| 流量采样 | 1 秒 | 1 秒后 | 读取接口计数并记录每日流量，未变化样本不发布事件 | 主要访问系统网卡；ICCID 缓存 15 秒，过期时可查询 SIM | `internal/application/network/service.go` |
| 短信刷新 | 3 秒 | 2 秒后 | 检查 SIM、读取模块短信存储、合并 SQLite 历史、发布新短信 | 每轮先调用 `IsSimInserted`，AT 模式会执行 `AT+QSIMSTAT?`，必要时回退 `AT+CPIN?` | `internal/application/sms/service.go` |
| 通话监控 | 3 秒 | 2 秒后 | 执行 `AT+CLCC`，识别通话状态并持久化记录 | 每轮访问 AT；设备 ICCID 在 `device.Service` 中缓存 1 分钟 | `internal/application/extras/service.go` |
| 网络状态刷新 | 15 秒 | 5 秒后 | 查询注册、运营商、网络制式、信号和 SIM | AT 模式执行 `AT+CEREG?`/`AT+CGREG?`/`AT+CREG?`、`AT+COPS?`、信号命令和 SIM 查询 | `internal/application/network/service.go` |
| 通知渠道恢复扫描 | 1 秒 | 1 秒后 | 检查失败渠道是否到达下一次恢复时间 | 不访问 modem | `internal/notify/manager.go` |
| AT 短信分片清理 | 2 分钟 | 2 分钟后 | 清理过期短信重组分片 | 不发 AT 命令 | `internal/modem/manager.go` |
| MBIM 分片回收 | 30 秒 | 30 秒后 | 清理超过 2 分钟仍未完成的 MBIM 响应收集器 | 仅 MBIM 后端存在 | `pkg/mbim/device.go` |

应用启动时统一启动 Runtime、短信、网络、通话、通知渠道和 VoWiFi 后台服务，见 `internal/app/app.go` 的 `App.Start`。

## 2. 连接维护任务

| 任务 | 周期或退避 | 生效条件 | 实现位置 |
| --- | ---: | --- | --- |
| WebSocket Ping | 30 秒 | 每个浏览器 WebSocket 连接 | `internal/api/http/server.go` |
| Runtime Trace SSE 保活 | 15 秒 | 打开运行时诊断流 | `internal/api/http/runtime_diagnostics.go` |
| Runtime Trace 合并刷新 | 25 毫秒 | 打开运行时诊断流 | `internal/api/http/runtime_diagnostics.go` |
| 通知命令监听重连 | 2 秒起，指数退避，最大 5 分钟 | Telegram 等命令监听中断 | `internal/notify/manager.go` |
| 通知渠道初始化恢复 | 每秒扫描，到期后按 2 秒至 5 分钟退避重建 | 渠道初始化发生可重试错误 | `internal/notify/manager.go` |
| Webhook 发送重试 | 请求失败后退避，次数由配置决定 | 网络错误或 HTTP 5xx | `internal/notify/webhook.go` |
| 前端 WebSocket 重连 | 3 秒起，指数退避至 30 秒并带随机抖动 | 浏览器事件流断开 | `web/src/stores/device.ts` |

## 3. 条件定时器

以下定时器只在对应操作发生时创建，不属于持续轮询：

| 定时器 | 时长 | 用途 |
| --- | ---: | --- |
| APDU 租约无进展看门 | 2 分钟 | 强制释放长时间无进展的 APDU transport lease |
| APDU transport 恢复期限 | 30 秒 | 强制释放后等待仍在飞行的 APDU 结束，否则隔离 transport |
| eSIM 确认码等待 | 5 分钟 | 下载过程中等待用户输入确认码 |
| VoWiFi 恢复防抖 | 2 秒 | 合并短时间内重复的网络/设备恢复触发 |
| 拨号建立等待 | 20 秒 | 等待通话轮询确认呼叫建立 |
| 前端终态 operation 清理 | 5 分钟 | 从浏览器状态中移除已结束 operation |
| macOS 拒接状态恢复 | 8 秒 | 防止拒接操作状态永久停留 |
| 固件 ADB 设置保存 | 输入停止后的短延迟 | 合并连续输入产生的保存请求 |

常规 AT 命令超时、HTTP 请求超时和关闭等待属于单次操作边界，不作为周期任务统计。

## 4. 前端页面周期任务

| 页面或状态 | 周期 | 行为 | 是否请求后端 |
| --- | ---: | --- | --- |
| 通话页离线兜底 | 15 秒 | WebSocket 未连接时刷新当前通话视图 | 是 |
| 短信页离线兜底 | 30 秒 | WebSocket 未连接时刷新短信视图 | 是 |
| eSIM 页离线兜底 | 60 秒 | WebSocket 未连接时刷新 eSIM 视图 | 是 |
| 网络页离线兜底 | 15 秒 | WebSocket 未连接时刷新网络视图 | 是 |
| VoWiFi 页离线兜底 | 30 秒 | WebSocket 未连接时刷新 VoWiFi 视图 | 是 |
| Runtime 诊断页 | 10 秒 | 刷新诊断快照 | 是，仅页面可见时 |
| Runtime 动画时钟 | 500 毫秒 | 更新拓扑动画 | 否 |
| 通话时长显示 | 1 秒 | 更新显示时长 | 否 |
| 流量曲线动画 | 每动画帧 | 平滑前端曲线 | 否 |

连接正常时，业务视图主要由 WebSocket 事件驱动；上述业务页面的固定间隔刷新是断线兜底，不是正常连接下的持续轮询。

## 5. 当前状态日志的触发链

观察到的日志形式：

```text
URC: 注册状态变更 {"type":"+CEREG","stat":5}
URC: SIM 插拔 {"type":"+QSIMSTAT","inserted":1}
URC: SIM 状态 {"type":"+CPIN","state":"READY"}
```

在修复前，AT 命令循环先用 `isURC` 判断所有响应行。除少数显式排除项外，任何以 `+` 开头的行都被视为 URC。因此同步查询及其响应会进入以下错误路径：

```text
短信刷新（每 3 秒）
  -> IsSimInserted()
  -> AT+QSIMSTAT?
  -> +QSIMSTAT: ...
  -> handleURC()
  -> 误记为“SIM 插拔”并触发 SIM 状态回调
  -> 又被“纯异步 URC”规则从命令响应中剔除
  -> QuerySIMInserted 无法解析 QSIMSTAT 结果
  -> 每轮继续回退执行 AT+CPIN?

网络刷新（每 15 秒）
  -> Radio() / SIM()
  -> AT+CEREG? / AT+QSIMSTAT?
  -> +CEREG: ... / +QSIMSTAT: ...
  -> handleURC()
  -> 误记为“注册状态变更”/“SIM 插拔”

QSIMSTAT 查询失败或响应无法解析
  -> 回退 AT+CPIN?
  -> +CPIN: READY
  -> handleURC()
  -> 误记为“SIM 状态”，并可能被当作 modem ready/reset 信号
```

因此，日志中的“变更”并不能证明卡或注册状态发生变化。3 秒短信轮询是 SIM 查询的主要稳定来源；修复前，每次成功返回的 `QSIMSTAT` 仍被剔除，通常会形成 `QSIMSTAT -> CPIN` 两条查询。15 秒网络轮询会额外产生注册和 SIM 查询。两者共享串行 AT 通道，执行耗时和竞争会让日志间隔出现轻微偏移。

## 6. 修复后的状态判定规则

修复遵循两级判定：

1. **命令上下文归属**：当活动命令是 `AT+CEREG?`、`AT+CGREG?`、`AT+CREG?`、`AT+QSIMSTAT?` 或 `AT+CPIN?` 时，对应前缀的响应留在当前命令结果中，不进入 URC 日志和回调路径。
2. **真实状态去重**：仅对脱离匹配命令上下文的真实 URC 进行状态比较。新值与最近一次成功查询或真实 URC 的已观测值相同，则不记录“变更”、不回调、不触发 ready/reset 或后续变更处理；只有未知初值或不同值才更新基线并处理一次。

无状态语义的异步事件，例如 `+CMTI`、`+CUSD`、`RING` 和 `+CLIP`，继续按原行为即时分发，不参与状态去重。

## 7. 与设备查询压力的关系

本次修复消除错误的变更日志和下游回调，并使有效的 `QSIMSTAT` 响应重新进入解析器，因此正常情况下不再额外回退执行 `AT+CPIN?`。现有轮询周期不变，短信轮询仍会每 3 秒执行一次 `AT+QSIMSTAT?`。若后续需要进一步降低 AT 查询压力，应独立评估：

- 为 `IsSimInserted` 增加短期缓存；
- 在收到真实 SIM URC 时使缓存失效；
- 让短信刷新直接复用最近的 SIM 状态，而不是每轮主动查询；
- 合并网络、短信和设备状态读取中的重复 SIM/身份请求。

这些属于轮询策略优化，不包含在本次响应分类与状态变更修复中。
