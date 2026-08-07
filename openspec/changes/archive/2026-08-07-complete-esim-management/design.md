# Design: Complete eSIM Management

## Context

`docs/ESIM_GAP_ANALYSIS.md` 确认五项 P0/P1 差距。底层 vendored `third_party/euicc-go` 与 `internal/esim/manager.go` 均已具备大部分能力：

- `Manager.DisableProfile(ctx, iccid, aidHex)`（manager.go:2327）实现完整但零调用；
- `Manager.DownloadProfile(..., progressFn DownloadProgressFn)`（manager.go:3142）已产出 `DownloadProgressEvent{Step, Msg, Pct}`（manager.go:2476-2483），但 `at_port.go:115` 传入 `nil`；
- `Manager.ListNotifications(aidHex)`（:3052）、`RetryNotification(seq, aidHex)`（:3083）有实现，无 API/UI；**`Manager.RemoveNotification` 不存在**，需新增（包内已有 `client.RemoveNotificationFromList` 用法，manager.go:2936）；
- euicc-go `DownloadOptions{OnConfirm, OnEnterConfirmationCode}`（lpa/download.go:104-110）未接线，manager 层也未暴露交互通道；
- 激活码解析已由 `ActivationCode.UnmarshalText`（lpa/download.go:47）完成，前端缺扫码入口。

约束：单设备运行；eSIM 操作经 `runtime.ResourceSIM` 锁串行化；异步操作统一走 `operation_id` + WebSocket 事件；前端能力由能力快照驱动（`CapabilityESIM`）。

## Goals / Non-Goals

**Goals:**
- Profile 停用全链路（接口 → 应用服务 → API → 前端），与启用对称
- 下载阶段进度上抛到 operation 事件（复用 manager 已有的 Step/Msg/Pct）
- 下载中确认码交互（请求 → 用户输入 → 继续/取消）
- eUICC 通知管理（列表/重发/删除）API + 前端面板
- 下载对话框二维码扫码/图片粘贴输入

**Non-Goals:**
- eUICC 重置（MemoryReset）、SM-DS 发现（ES11）、QMI/MBIM eUICC 传输启用、PPR/图标显示、PKI 证书链校验、`esim.changed` 生产者（见 proposal 非目标，留待后续变更）
- 摄像头实时扫码（第一版只做图片选择 + 剪贴板粘贴，避免桌面端权限与窗口焦点复杂度）

## Decisions

### D1: `ESIMPort.Download` 扩展为带选项的交互式签名

**现状**：`Download(ctx, activationCode, confirmationCode, matchingID string) error`；`at_port.Download` 传 nil progressFn。

**决策**：改为
```go
type ESIMDownloadOptions struct {
    Progress func(step string, pct int, msg string) // manager DownloadProgressEvent 透传
    ConfirmationCodeRequest func() (code string, canceled bool, err error) // 阻塞等待用户输入
}
Download(ctx context.Context, activationCode, confirmationCode, matchingID string, opts *ESIMDownloadOptions) error
```
**理由**：进度与确认码交互天然属于"下载中"的两路信号（one-way 事件 + two-way 请求），塞进返回值或全局事件流都会失去与 operation 的绑定。可选结构体指针保证 QMI/MBIM 未来端口可忽略该参数。**替代方案**（保持接口不变、应用服务轮询状态）被否：引入额外轮询状态机，复杂度高于直接透传回调。

### D2: 确认码交互经 operation 事件通道实现，回复走独立 API

**流程**：
1. 应用服务 `Download` 在 `ops.Start` 内调用 `port.Download(..., opts)`；
2. manager 层新增交互透传（见 D3），SM-DP+ 要求确认码时调用 `opts.ConfirmationCodeRequest`；
3. 应用服务用 `ops.Publish` 发布 `esim.confirmation_code_request` 事件（携带 `operation_id`、`request_id`），然后阻塞在 channel 上；
4. 新增 `POST /api/v1/esim/operations/{operation_id}/confirmation-code`，body `{code}`（空串或显式 `declined:true` 视为取消），handler 经 map[requestID]chan 注入回复；
5. 超时（固定 5 分钟，与操作超时对齐）未回复 → 取消下载并发布结构化取消结果。

**理由**：复用现有 operation/WS 基建（前端已绑定 OperationStatusView），无需新的事件通道。取消语义与 spec 中"用户拒绝或超时 → 干净取消"一致。**替代方案**（WebSocket 反向请求）被否：WS 目前是单向事件流，加反向消息需要改动事件协议，收益低于一个 REST 回复端点。

### D3: manager 层新增下载交互参数与 RemoveNotification

- `DownloadProfile` 签名扩展：增加可选 `interact *DownloadInteraction` 参数（`OnProgress` 已存在 + 新增 `OnConfirmationCodeRequest func() (string, bool, error)`），内部构造 euicc-go `DownloadOptions`（lpa/download.go:104-110）时填入 `OnEnterConfirmationCode`/`OnConfirm`；调用点为 at_port.go:115 一处，测试调用点同步更新。
- 新增 `Manager.RemoveNotification(sequenceNumber int64, aidHex string) error`：包装 `client.RemoveNotificationFromList`，签名与 `RetryNotification`（:3083）镜像。**风险**：`DeleteProfile`/`autoCleanLoadedNotifications` 内已有 remove 逻辑（manager.go:2936），新方法必须与既有路径共用同一 `client` 会话语义，不重复加锁（外层由调用方持有）。

### D4: Disable 全链路走既有模式镜像 Enable

- `ESIMPort` 增加 `Disable(ctx, iccid) error`（service_ports.go:18-25）；`at_port.Disable` 调 `p.manager.DisableProfile(ctx, strings.TrimSpace(iccid), "")`（manager.go:2327，签名与 `SwitchProfile` 对称）；
- 应用服务新增 `Disable`：`ops.Start("esim.disable")` + `runtime.Acquire(ResourceSIM)` + `port.Disable` + `ops.Publish("esim.updated", {operation: "disable", iccid})`，镜像 `Enable`（service.go:91-110）；
- API 新增 `POST /api/v1/esim/actions/disable`，请求体复用 `esimRequest{iccid}`，返回 `{operation_id}`（镜像 enable 路由 server.go:291）；OpenAPI 同步；
- 前端：`EsimView.vue` 在 `state === 'enabled'` 时显示"停用"按钮（与现有"启用仅当 disabled"对称，:141-142），`stores/esim.ts` 加 `disable` action，`api.ts` 加 `esimDisable`。

### D5: 进度上抛直接透传 manager 已产出的 Step/Msg/Pct

`at_port.Download` 收到 `opts.Progress` 后原样转发；应用服务在 `ops.Start` 的 progress 回调里把 `(pct, msg)` 映射为 operation 进度事件（现有 `progress(5, "preparing")`/`progress(100, ...)` 模式，service.go:81-85）。**零新逻辑**——manager 已产出中文阶段描述，UI 的 OperationStatusView 无需改动，只需保证事件携带阶段文案。

### D6: 通知管理 API 与前端面板

- 应用服务新增 `ListNotifications() / ProcessNotification(seq) / RemoveNotification(seq)`，镜像 `Manager.ListNotifications(aidHex)` / `RetryNotification` / 新 `RemoveNotification`（aidHex 用管理器的默认/扫描结果，与 Overview 路径一致）；通知操作不持 ResourceSIM 锁（读路径 + 网络重发，避免与下载互相饿死，重发失败不改变卡片状态）。
- API：
  - `GET /api/v1/esim/notifications` → `NotificationItem[]`（manager.go:2564-2571 结构直接序列化：sequence_number/event/iccid/address/can_retry）
  - `POST /api/v1/esim/notifications/{seq}/process` → 重发结果或结构化错误（失败保留通知）
  - `DELETE /api/v1/esim/notifications/{seq}` → 删除确认或 `invalid_sequence` 错误
  - 能力不足时返回 `capability_not_supported`（对齐 device-api spec 场景）
- 前端：EsimView 设置区新增"通知"面板，列表 + 每项"重发/删除"按钮，内联显示结果/错误；`stores/esim.ts` 加通知状态与 actions。

### D7: 二维码输入用 `jsqr` + 文件/剪贴板，不引摄像头依赖

- 下载对话框新增"扫描二维码"按钮 → `<input type="file" accept="image/*">` 选择图片，同时监听 `paste` 事件读取剪贴板图片；
- 用 `jsqr`（纯 JS 解码，无原生依赖）解码，得到 `LPA:1$...` 字符串填入激活码输入框（解析验证仍由后端 `ActivationCode.UnmarshalText` 兜底，前端只做格式提示）；
- 保留手动文本输入（现有行为不变）。
- **理由**：桌面管理页常驻于 127.0.0.1，摄像头权限与多窗口焦点问题复杂；图片选择 + 粘贴覆盖绝大多数"运营商邮件/网页给二维码"场景。**替代方案** `@zxing/browser`（摄像头）留作后续增强。

## Risks / Trade-offs

- [D2 超时窗口内用户无输入 → 下载取消] → 取消结果结构化、可重试，spec 已定义"超时 → 干净取消"；取消时 manager 已有 abort/cancelSession 路径（download.go:262-270）
- [D3 扩展 `DownloadProfile` 签名 → 测试调用点批量改动] → 全仓库调用点仅 at_port.go:115 与测试文件，改动面受控；参数设计为指针可选避免破坏性
- [D6 通知删除不可逆（GSMA 语义上删除即不再上报）] → UI 删除按钮需确认文案；EasyLPAC 同类操作同样不设撤销，属业界一致行为
- [D7 jsqr 对模糊/低对比二维码识别率有限] → 解码失败给出"请重新拍摄或手动输入"引导；手动输入路径始终可用
- [前端新增依赖（jsqr）] → 体积小（~30KB）、无运行时代理需求，符合 web/ 现有依赖风格

## Migration Plan

1. 后端先行：ESIMPort 签名扩展 + manager 交互参数（D1-D3）→ 跑 `go test ./internal/esim ./internal/backend ./internal/application` 保持全绿
2. 应用服务 + API（D4、D6 后端部分）→ OpenAPI 与 server.go 同步
3. 前端（D4/D5/D6 前端部分 + D7）→ `npm --prefix web run typecheck && lint && build`
4. 更新 `docs/ESIM_GAP_ANALYSIS.md` 差距状态与 README 功能列表
5. 无数据库/存储变更，无迁移；回滚 = 撤销对应 commit，接口为增量扩展不破坏旧客户端
