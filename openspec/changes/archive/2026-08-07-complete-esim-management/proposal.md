# Complete eSIM Management

## Why

`docs/ESIM_GAP_ANALYSIS.md` 对比业界主流 LPA（lpac/EasyLPAC、Android LPA、Windows 内置等）发现：本项目 SGP.22 下载/启用/删除/改名核心链路已完整，但 **Profile 停用、下载阶段进度、确认码交互、通知管理、二维码扫描** 五项业界标配能力缺失或未接线（底层 vendored `euicc-go` 库均已支持，多数只需接线）。本变更补齐这五项 P0/P1 差距，使 eSIM 管理面达到主流工具水准。

## What Changes

- **Profile 停用（Disable）**：后端 `Manager.DisableProfile` 已有实现（manager.go:2327）但全链路零调用。补 `ESIMPort.Disable`、应用服务 `Disable`、`POST /api/v1/esim/actions/disable` 端点及前端"停用"操作按钮，与现有"启用"对称。
- **下载阶段进度上抛**：`at_port.go` 当前向 manager 传入 `nil` progressFn，应用服务仅上报 5%/100%。改为透传 manager 的 `DownloadProgressFn`（auth_client → auth_server → install → notify 阶段）到 operation progress 事件，前端已有 OperationStatusView 可展示。
- **下载确认码交互**：euicc-go 的 `OnConfirm`/`OnEnterConfirmationCode` 回调当前未使用，确认码缺失时下载直接中止。改为下载中若服务端要求确认码，通过 operation 事件向 UI 请求输入并暂挂，用户输入后继续。
- **通知管理（API + UI）**：`Manager.ListNotifications`/`RetryNotification` 已有实现但无 API/UI。新增通知查询、重发、删除接口与前端管理面板。
- **二维码扫描**：激活码解析已由 `ActivationCode.UnmarshalText` 完成，缺扫码入口。下载对话框增加二维码扫码（含图片粘贴）输入，保留手动文本输入。
- **非目标**（后续变更）：eUICC 重置（MemoryReset）、SM-DS 发现（ES11）、QMI/MBIM eUICC 传输启用、Profile 图标/PPR 显示、PKI 证书链校验、`esim.changed` 事件生产者。

## Capabilities

### New Capabilities
- `esim-notifications`: eUICC 待处理通知的查询、重发（process）与删除（remove）管理能力

### Modified Capabilities
- `device-services`: eSIM 服务新增 Disable 操作；下载操作细化进度阶段并支持下载中确认码交互
- `device-api`: `/api/v1/esim` 新增 disable 操作端点与 notifications 资源端点
- `vue-management-ui`: eSIM 视图新增停用操作、二维码扫描、确认码对话框与通知管理面板

## Impact

- **后端**：`internal/backend/service_ports.go`（ESIMPort 扩展）、`internal/application/esim/service.go`、`internal/esim/at_port.go`（progressFn/确认码回调）、`internal/api/http/server.go`（路由与 OpenAPI）、`internal/esim/manager.go`（已有实现接线，需少量事件/回调适配）
- **前端**：`web/src/views/EsimView.vue`、`web/src/stores/esim.ts`、`web/src/i18n.ts`（新增停用/通知/扫码文案）、`web/src/types.ts`、`web/src/services/api.ts`；新增二维码解析依赖
- **事件**：operation 事件模型扩展（确认码请求事件）
- **测试**：`internal/backend/at_backend_esim_test.go` 等既有测试扩展；新增 disable/notifications API 测试与前端类型校验
- **文档**：更新 `docs/ESIM_GAP_ANALYSIS.md` 差距状态与 README 功能列表
