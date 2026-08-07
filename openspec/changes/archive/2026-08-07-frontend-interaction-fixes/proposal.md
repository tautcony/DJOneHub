# 前端交互与功能缺陷修复

## Why

对 `web/`（Vue 3 + TS + Ant Design Vue + Pinia）代码审查发现若干**功能与交互层面的实质缺陷**，其中最高优先级的会在「直接打开/刷新某个视图」时造成不可逆的不良体验。现有 `vue-management-ui` 规范强调「能力驱动」「异步操作与事件」「状态有界」，但本次审查发现实现与规范之间存在落差：

- **初始视图从不自动加载**：`App.vue` 的 `onMounted`（App.vue:1158-1164）只调用 `device.refresh()` 与 `device.connect()`，未触发 `loadView(active.value)`；`watch(active, …)`（App.vue:953）未设 `immediate`，而 `device.status.changed` 分支（App.vue:535-539）仅调度 `overview` 与 `firmware`。结果：直接访问 `/sms`、`/esim`、`/network`、`/calls`、`/vowifi`、`/settings`、`/notifications` 等并刷新时，视图内的 `LoadingState`（`loadedViews[view]` 永不置真）**永久停在加载中**，除非后端恰好推送对应领域事件。
- **已接线却无入口**：`api.smsClear`、`sms.clear()`、`clearModuleSMS`（App.vue:352）、`context.ts:64` 与 `common.clear` 文案均就绪，但无任何模板调用 → 「清空模块短信」成为死功能。
- 其余为交互打磨（流量周/月无 loading 态、操作弹窗 5 分钟 TTL 后变空白、语言/敏感开关误弹「设置已保存」、死代码与重复工具函数）。

> 注：审查初稿曾计划「按能力在导航层隐藏无能力项」，但实现后发现会让菜单在设备未就绪（能力快照为空）时塌缩为仅概览/通知/设置三项，体验更差。故**导航项保持常驻显示**，能力缺失改由各视图内部处理（`device.has` 门控、RawAtView 的「不可用」提示等），与现有交互一致。

## What Changes

- **初始视图自动加载**：`onMounted` 中补充 `void loadView(active.value)`（或给 `watch(active)` 加 `immediate: true`）；并让 `device.status.changed` 也调度当前激活视图，使首屏与刷新后任意视图都能拿到数据。
- **导航项常驻显示（不做能力门控）**：保留全部导航项可见，能力缺失交由各视图内部处理（`device.has` 门控、RawAtView「不可用」提示等），避免在设备未就绪时菜单塌缩。
- **清空模块短信入口**：在 SMS 会话列表区补充「清空」操作按钮，调用已有的 `clearModuleSMS`；保留刷新后收件箱缓存的既有语义。
- **流量周/月加载态**：切换 `trafficRange` 到非 `day` 时带 `showLoading` 反馈，避免空表无提示。
- **操作弹窗/卡片在打开期间保持稳定**：固件操作弹窗与 SMS 操作指示在打开/进行期间保留 operation 快照，避免 5 分钟 TTL 清理后突然变空白（不破坏既有的「有界」清理策略）。
- **提示文案收敛**：语言切换使用独立提示；敏感信息开关保持「设置已保存」语义，避免误用。
- **代码清理**：移除 App.vue 中未使用的 store 解构（`smsItems`/`smsSentItems`/`smsThreads`/`esimNotes`/`esimHealth`）；将 `mask`/`bytes`/`rate` 重复实现抽到 `web/src/utils/`。

## Capabilities

### Modified Capabilities
- `vue-management-ui`：新增「首屏/激活视图自动加载」「操作面在可见期间稳定」「流量区间加载反馈」等行为要求；保留「能力驱动」与「状态有界」不变（导航项保持常驻显示，能力门控继续落在视图内部）。

### New Capabilities
（无新增能力；本次为既有 UI 的行为修复，不引入新的后端/前端能力。）

## Impact

- **前端**：`web/src/App.vue`（onMounted、watch(active)、device.status.changed 调度、提示文案、移除死解构；导航保持常驻显示）、`web/src/views/SmsView.vue`（清空按钮）、`web/src/views/OverviewView.vue`（周/月加载态）、`web/src/views/FirmwareView.vue` + `web/src/stores/device.ts`（操作快照稳定）、`web/src/utils/`（抽出 `bytes`/`rate`）、`web/src/i18n.ts`（新增/微调文案）。
- **规范**：更新 `openspec/specs/vue-management-ui/spec.md` 的对应要求（见本变更 `specs/` 增量）。
- **测试**：`npm --prefix web run typecheck && lint && build` 保持绿色；建议补充「直接打开 /sms 刷新后加载完成」的人工/单元校验点。
- **后端**：无变更。
