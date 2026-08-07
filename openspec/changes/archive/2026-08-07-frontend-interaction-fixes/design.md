# Design: 前端交互与功能缺陷修复

## Context

`web/` 采用「shell 编排 + 领域 store + 类型化 ViewContext」架构（见 `vue-management-ui` 规范）。本次审查定位的问题大多源于**编排层（App.vue）与导航层（AppShell.vue）对规范意图的实现缺口**，而非架构错误：

- 各视图的加载逻辑完整（`loadView`/`viewLoaders`、各 `loadXxx`、`markViewLoaded`），但**唯一的首屏触发点是 `onMounted`，而它漏掉了 `loadView(active.value)`**；`active` 的 `watch` 未 `immediate`，因此首屏视图依赖「后端恰好推送对应事件」才能拿到数据，属脆弱的后端行为耦合。
- 导航项在 `App.vue:148-177` 已带 `capability` 元数据，但 `AppShell` 渲染时未消费，导致能力门控在导航层失效。规范「能力驱动」目前只在视图内部（按钮 `:disabled="device.has(...)"`）落实。
- 「清空模块短信」「语言切换提示」等属于已具备后端/状态能力、仅缺前端接线或文案纠偏。

约束：单设备运行；视图数据来自领域 store 经 `ViewContext` 注入；能力由能力快照驱动；异步操作走 `operation_id` + WebSocket 事件；保持「状态有界」（终态 operation 5 分钟后清理，device.ts:122-133）。

## Goals / Non-Goals

**Goals:**
- 修复首屏/刷新后任意激活视图自动加载（最高优先级，消除永久加载圈）
- 导航按能力门控，无能力项降级为内联说明而非 403
- 补上「清空模块短信」UI 入口
- 流量周/月区间切换带加载反馈；操作面在可见期间不闪空；提示文案收敛；清理死代码与重复工具

**Non-Goals:**
- 不改动后端、不改 `device.ts` 的 TTL 清理策略（仅在使用侧增加「可见期间快照」）
- 不新增能力/端点（本次纯前端行为修复）
- 不重写现有 `loadView` 调度/节流机制，仅在缺漏处补触发

## Decisions

### D1: 首屏加载用 `onMounted` 显式触发，而非 `immediate` watch

`watch(active, …, { immediate: true })` 与 `onMounted` 末尾 `void loadView(active.value)` 都能触发首次加载。选择 **`onMounted` 显式调用**，因为它与既有的 `device.refresh()` + `device.connect()` 编排同处一处，可读性更好，且不会让 `active` 的每次初值变化都产生副作用（路由重定向 `/`→`/overview` 已先完成）。`device.status.changed` 分支同步扩展为「若激活视图非 overview/firmware 也调度之」，使状态变化同样刷新当前视图。

### D2: 导航能力门控放在 `AppShell` 渲染层

`navGroups` 已带 `capability`，`AppShell` 已有 `deviceCapabilities` 吗？——目前 App.vue 把 `deviceCapabilities` 提供进 context 但未传给 AppShell。决策：将 `capability` 过滤逻辑放进 `App.vue` 计算导航可见项（或给 AppShell 增加 `deviceCapabilities` prop），由 `AppShell` 对无能力项做隐藏；保留「完全无能力就隐藏整组」的简化策略，避免半隐藏的混乱。`RawAtView` 已有的「不可用」内联说明作为降级范式。

### D3: 清空短信入口位置与语义

在 SmsView 会话列表头部操作区增加「清空」按钮，调用 `clearModuleSMS()`。依据现有 `sms.cleared` 文案「ME 存储已清理；收件箱缓存仍保留」，清空后端 ME 存储但保留前端缓存，符合既定语义。按钮仅在 `device.has('sms_read')` 时可用。

### D4: 操作面可见期间保留快照

`firmwareOperation` 读 `device.operations[firmwareOperationID]`（App.vue:113-118）。修复：在打开固件操作弹窗（`watch(firmwareOperationModalOpen)`）与 SMS 操作进行期间，将 operation 终态/进行态复制到局部 `ref`，模板优先读局部快照；关闭弹窗或发送完成后清空。不动 `device.ts` 的 TTL。

### D5: 工具函数收敛到 `web/src/utils/`

新增 `web/src/utils/format.ts`：`formatBytes(value)`、`formatRate(value)`、`maskSensitive(value, show)`；`OverviewView` 与 `NetworkView` 改为引用，删除本地重复实现；`OverviewView` 的本地 `mask` 统一改用 context 的 `maskSensitive`（与其余视图一致）。
