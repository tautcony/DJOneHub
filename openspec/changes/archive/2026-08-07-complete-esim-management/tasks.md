## 1. 后端核心：ESIMPort 交互式下载签名与 manager 能力

- [x] 1.1 在 `internal/backend/service_ports.go` 新增 `ESIMDownloadOptions` 结构体（`Progress func(step string, pct int, msg string)`、`ConfirmationCodeRequest func() (code string, canceled bool, err error)`），并将 `ESIMPort.Download` 签名扩展为 `Download(ctx, activationCode, confirmationCode, matchingID string, opts *ESIMDownloadOptions) error`
- [x] 1.2 在 `internal/esim/manager.go` 扩展 `DownloadProfile`：新增可选 `interact` 参数（含 `OnConfirmationCodeRequest`），内部构造 euicc-go `DownloadOptions`（lpa/download.go:104-110）时填入 `OnProgress`/`OnEnterConfirmationCode`/`OnConfirm`；确认码请求阻塞等待用户输入，取消返回错误走既有 abort 路径
- [x] 1.3 新增 `Manager.RemoveNotification(sequenceNumber int64, aidHex string) error`，包装 `client.RemoveNotificationFromList`（manager.go:2936 同语义），签名与 `RetryNotification`（:3083）镜像
- [x] 1.4 更新 `internal/esim/at_port.go`：`Download` 透传 opts（progressFn 不再传 nil）；`ESIMPort` 新增 `Disable(ctx, iccid string) error` 实现（调 `p.manager.DisableProfile(ctx, strings.TrimSpace(iccid), "")`）
- [x] 1.5 更新 `internal/backend/at_backend.go` 的 Download/Disable 委托，与既有 ESIMPort 注入模式一致
- [x] 1.6 修正全部受签名扩展影响的测试调用点（`at_backend_esim_test.go`、manager 相关测试），`go test ./internal/...` 全绿

## 2. Profile 停用全链路

- [x] 2.1 应用服务 `internal/application/esim/service.go` 新增 `Disable(ctx, iccid) (string, error)`：镜像 `Enable`（service.go:91-110），`ops.Start("esim.disable")` + `runtime.Acquire(ResourceSIM)` + `port.Disable` + `ops.Publish("esim.updated", {operation: "disable", iccid})`
- [x] 2.2 `internal/api/http/server.go` 新增 `POST /api/v1/esim/actions/disable`（复用 `esimRequest{iccid}`，返回 `{operation_id}`，镜像 enable 路由 :291），OpenAPI 声明同步（openapi.go）
- [x] 2.3 前端 `web/src/services/api.ts` 新增 `esimDisable`；`web/src/stores/esim.ts` 新增 `disable` action 与操作状态；`web/src/views/EsimView.vue` 在 `state === 'enabled'` 时显示"停用"按钮（与现有"启用"对称 :141-142），i18n 文案补充（i18n.ts esim 命名空间）

## 3. 下载进度与确认码交互

- [x] 3.1 应用服务 `Download` 将 `opts.Progress`（manager 已产出的 Step/Pct/Msg，manager.go:2476-2483）映射为 operation 进度事件，替代现有 5%/100% 硬编码（service.go:81-85）
- [x] 3.2 应用服务确认码交互：`ConfirmationCodeRequest` 内 `ops.Publish("esim.confirmation_code_request", {operation_id, request_id})` 后阻塞等待回复 channel；超时（5 分钟）未回复按取消处理
- [x] 3.3 `internal/api/http/server.go` 新增 `POST /api/v1/esim/operations/{operation_id}/confirmation-code`（body `{code}`；空串/`declined:true` 视为取消），经 request_id 注入回复
- [x] 3.4 前端：OperationStatusView/下载对话框监听 `esim.confirmation_code_request` 事件 → 弹出确认码输入框 → 提交到 3.3 端点或取消；i18n 文案补充

## 4. 通知管理

- [x] 4.1 应用服务 `internal/application/esim/service.go` 新增 `ListNotifications`、`ProcessNotification(seq)`、`RemoveNotification(seq)`（调 `Manager.ListNotifications`/`RetryNotification`/新 `RemoveNotification`；不持 ResourceSIM 锁）
- [x] 4.2 API：`GET /api/v1/esim/notifications`（返回 `NotificationItem[]`，manager.go:2564-2571 直接序列化）、`POST /api/v1/esim/notifications/{seq}/process`、`DELETE /api/v1/esim/notifications/{seq}`；能力不足返回 `capability_not_supported`；OpenAPI 同步
- [x] 4.3 前端：EsimView 新增"通知"面板（列表 + 每项"重发/删除"，内联结果/错误）；`stores/esim.ts` 通知状态与 actions；`api.ts` 三个端点；i18n 文案

## 5. 二维码输入

- [x] 5.1 `web/package.json` 新增 `jsqr` 依赖
- [x] 5.2 下载对话框新增"扫描二维码"：文件选择（`accept="image/*"`）+ 剪贴板 `paste` 事件 → jsqr 解码 → 解析出 `LPA:1$...` 填入激活码输入框；解码失败给出引导文案（保持手动输入可用）

## 6. 测试与文档收尾

- [x] 6.1 后端测试：disable 操作（接口/应用服务/at_port）、下载进度回调、确认码请求/取消/超时、通知 list/process/remove（含失败保留通知）；`go test ./...` 全绿
- [x] 6.2 前端校验：`npm --prefix web run typecheck && lint && build`
- [x] 6.3 更新 `docs/ESIM_GAP_ANALYSIS.md`（P0/P1 项标记为已接线）与 README 功能列表
