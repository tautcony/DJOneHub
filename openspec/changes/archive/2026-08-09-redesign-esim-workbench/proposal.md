## Why

`complete-esim-management` 已将 Profile 停用、下载阶段进度、动态确认码、通知管理和二维码输入接入现有 eSIM 全链路，但当前页面仍把这些能力平铺在一个长页面中，功能虽存在却难以发现、理解和连续操作。本变更以交互重构为主，并要求在替换页面前先用合同测试和演示流程确认每个准备呈现的能力真实存在、响应有效且状态能够回收。

## What Changes

- 在实现界面重构前建立现有 eSIM 能力基线，逐项验证 Profile 查询、下载、启用、停用、改名、删除、本地备注、通知查询/重发/删除、通知历史、下载进度和动态确认码。
- 将现有 eSIM 路由重构为一个页内工作台，以 `Profile` 和 `通知` 两个主工作区组织已有功能；不增加全局导航，不影响其他页面。
- 增加基于现有 Profile 数据的搜索、状态筛选、清晰状态表达、上下文操作和与通知的双向定位。
- 将待处理通知与本地通知历史分开呈现，增加基于现有数据的搜索和筛选；继续使用已验证的单项重发、单项删除接口。
- 重构 Profile 下载交互，将手动输入、二维码文件、剪贴板图片、拖放、下载进度和动态确认码组织为一个连续流程，但不改变现有下载 API 和 operation 事件协议。
- 增加统一的 eSIM 操作状态区域，确保启用、停用、删除和下载接受后持续显示 operation 进度、终态、错误和刷新结果。
- 明确区分 eUICC 昵称与 DJOneHub 本地备注，强化删除 Profile 和移除通知的不可逆提示，并保持敏感标识脱敏。
- 不在本变更中新增 eUICC 富详情、默认 SM-DP+ 编辑、通知行为偏好、通知批处理、SM-DS、eUICC 重置或摄像头扫码；MiniLPA 中没有现有有效接口支撑的功能不得只做一个无效入口。

## Capabilities

### New Capabilities

- `esim-management-workbench`: 定义基于已验证现有能力的单页 eSIM 交互工作台、Profile/通知任务流、连续下载体验和操作状态反馈。

### Modified Capabilities

无。

## Impact

- 主要影响前端：`web/src/views/EsimView.vue`、`web/src/stores/esim.ts`、`web/src/services/api.ts`、`web/src/types.ts`、`web/src/i18n.ts`、`web/src/style.css`，并新增或拆分 eSIM 领域组件。
- 后端原则上不增加产品能力或公共端点；只补齐现有行为的应用服务/API 合同测试，并修复验证中发现的阻断性缺陷。
- 继续复用现有 `/api/v1/esim`、Profile action、通知、通知历史、确认码回复和 operation 查询端点。
- 继续使用现有 Vue 3、Pinia、Ant Design Vue、`jsqr` 和 WebSocket operation 基础设施，不新增 UI 框架或扫码依赖。
