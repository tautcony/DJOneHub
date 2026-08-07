## 1. 首屏/激活视图自动加载（关键）

- [x] 1.1 在 `App.vue` 的 `onMounted`（App.vue:1158-1164）末尾补充 `void loadView(active.value)`，确保刷新/直接打开任意视图都能触发其 loader（`loadView` 内部已用 `isActiveView` 守卫，首屏 `pageVisible` 为真、`active` 为当前路由，安全）
- [x] 1.2 扩展 `App.vue` 的 `device.status.changed` 分支（App.vue:535-539）：除 `overview`/`firmware` 外，对其它激活视图也 `scheduleViewRefresh(active.value)`，使状态变化同样刷新当前视图
- [x] 1.3 验证：直接访问 `/sms`、`/esim`、`/network`、`/calls`、`/vowifi`、`/settings`、`/notifications` 并刷新，各视图均能在无领域事件推送的情况下完成加载（`loadedViews[view]` 置真，加载圈消失）

## 2. 导航能力门控（高）

- [x] 2.1 在 `App.vue` 计算导航可见项时，依据 `capability` 与 `deviceCapabilities`（App.vue:202）过滤 `navGroups`/`nav`；无对应能力的项整体隐藏（沿用 RawAtView「不可用」降级范式，避免无能力用户点进去触发 403）
- [x] 2.2 将过滤后的导航传给 `AppShell`（必要时为 `AppShell` 增加 `deviceCapabilities` prop 或在 `App.vue` 完成过滤），`AppShell.vue:105-114` 仅渲染可见项
- [x] 2.3 验证：以缺少 `raw_at`/`sms_read`/`network_status` 等能力的快照进入，对应导航项不显示；具备能力时正常显示并可进入

## 3. 清空模块短信入口（高）

- [x] 3.1 在 `SmsView.vue` 会话列表头部操作区增加「清空」按钮，调用 context 的 `clearModuleSMS()`，仅在 `device.has('sms_read')` 时可用
- [x] 3.2 清空成功后沿用现有 `sms.cleared` 文案（「ME 存储已清理；收件箱缓存仍保留」），保留前端缓存语义
- [x] 3.3 验证：点击清空 → 后端 ME 存储清空、前端收件箱缓存保留、结果提示正确

## 4. 流量周/月加载态（中）

- [x] 4.1 在 `OverviewView.vue` 的 `watch(trafficRange)`（OverviewView:161-168）切换到非 `day` 时带 `showLoading` 反馈；`refreshTrafficRange` 已在签名中支持 `showLoading`，确保空表期间显示 loading 而非空白
- [x] 4.2 验证：概览页切到「周/月」时区间表格出现加载指示，数据返回后正常渲染

## 5. 操作弹窗/卡片在可见期间稳定（中）

- [x] 5.1 在 `App.vue` 为固件操作弹窗增加局部 operation 快照：`watch(firmwareOperationModalOpen)` 打开时复制当前 `firmwareOperation` 到 `firmwareOperationSnapshot` ref，模板优先读快照；关闭时清空（不动 `device.ts` TTL 清理，device.ts:122-133）
- [x] 5.2 在 `SmsView.vue` 的发送进行/完成后，将 `smsOperation` 终态保留到局部，避免 5 分钟 TTL 清理后指示器突然消失（仍由 `selectThread` 复位）
- [x] 5.3 验证：打开固件操作弹窗并静置超过 5 分钟，operation 详情仍可见；SMS 发送成功后指示在合理时间内稳定显示

## 6. 提示文案收敛（中）

- [x] 6.1 在 `App.vue` 的 `watch(locale)`（App.vue:224-228）改用独立提示（如「语言已切换」/「Language switched」），不再复用 `settings.saved`
- [x] 6.2 在 `web/src/i18n.ts` 两个语言包新增对应文案键（如 `settings.languageSwitched`）
- [x] 6.3 验证：切换语言弹出「语言已切换」，切换敏感信息开关仍弹「设置已保存」

## 7. 代码清理（低）

- [x] 7.1 移除 `App.vue` 中未使用的 store 解构：`smsItems`、`smsSentItems`、`smsThreads`、`esimNotes`、`esimHealth`（App.vue:64-65、72、89-90）
- [x] 7.2 新增 `web/src/utils/format.ts`：`formatBytes`、`formatRate`、`maskSensitive`；`OverviewView.vue`（62-79）与 `NetworkView.vue`（23-32）改用之，删去本地重复实现；`OverviewView` 本地 `mask` 统一改用 context 的 `maskSensitive`
- [x] 7.3 验证：`npm --prefix web run typecheck && lint && build` 保持绿色，无 ESLint 未使用变量告警
