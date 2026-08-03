# DJOneHub 前端重构 Task

状态：实现完成，已完成构建与 Toast 浏览器验证
日期：2026-08-03

## 目标

重做 `web/` 的信息架构、页面布局和视觉层级，使它参考 `vohive-open/web` 的管理后台体验：有清晰的应用壳层、统一的页面标题、状态灯、字段行、面板、加载态、空态和错误态，并在桌面端与移动端都能稳定使用。

本次只改善现有 DJOneHub 功能的展示和交互组织，不扩展产品能力，不把 DJOneHub 改造成完整的 VoHive 管理后台。

## 范围边界

### 保留的现有能力

以下能力以当前 `web/src`、`/api/v1` 和已有 DTO 为准，重构后都必须继续可用：

| 页面 | 保留功能 |
| --- | --- |
| 概览 | 单设备连接状态、SIM、IMEI、ICCID、信号、驻网、运营商、网络制式、backend、capability |
| 通话 | 当前通话、通话历史、拒接当前来电、轮询状态 |
| 短信 | 收件箱、按联系人聚合、搜索、会话查看、发送、长短信分段展示、验证码复制、刷新、清理模块旧短信、发送 operation 进度 |
| eSIM | EID、Profile 列表、下载、启用、改名、删除、Profile 状态、eSIM 健康状态、通讯录兼容性、Profile 本地备注和模块通讯录备注、operation 进度 |
| 网络 | USB 网络模式、无线制式、网卡、地址、默认路由、实时收发速度、本次流量、4G/代理检查、蜂窝策略、切换模式、模块重启 |
| GPS | GPS 开关、最后定位、刷新、错误状态和轮询 |
| AT 调试 | capability 检查、常用指令预设、发送指令、结构化解析结果和原始响应 |
| VoWiFi | 可用性、状态、原因、启用、禁用、重连、operation 进度和不支持原因 |
| 设置 | 语言选择和敏感标识显示开关 |

### 明确不新增的功能

参考端已有但当前 DJOneHub 没有的内容不进入本次重构：

- 登录、鉴权页面和远程多租户管理。
- 多设备列表、设备注册/接管、设备配额、设备切换和设备池。
- 代理实例、上游代理、国家路由、代理流量分析和代理控制面板。
- 日志页面、通知渠道、机器人、Webhook、邮件和推送配置。
- USSD、运营商扫描、运营商网页弹窗等当前 API 没有的操作。
- 历史流量图表、ECharts 分析和当前 API 没有的统计数据。
- 参考端设备卡片里的公网 IP、切换 IP、设备名等当前页面没有对应数据或契约的展示；现有网络页的“模块重启”操作继续保留，但不新增额外快捷入口。
- 任何需要新增后端路由、DTO、数据库字段或硬件能力的按钮。

视觉上的深色主题也不在本次新增范围内：当前 `web/` 只保留已有的语言选择和敏感标识开关，不因为参考端有 `SwitchDark` 就额外添加主题设置。

## 参考借鉴清单

只借鉴实现方式和信息组织，不直接复制完整 VoHive 产品页面：

| `vohive-open/web` 参考 | DJOneHub 采用方式 | 适配限制 |
| --- | --- | --- |
| `layouts/AuthenticatedShell.vue` | 建立单设备版 `AppShell`：侧栏、品牌、连接状态、重新扫描、主内容区 | 不引入登录、路由鉴权和多设备选择 |
| `components/PageHeader.vue` | 每个页面统一标题、说明和右侧操作区 | 标题和操作只来自 DJOneHub 已有页面 |
| `components/StatusLight.vue` | 统一表达连接、设备、驻网、Profile、检查结果和 VoWiFi 状态 | 状态值来自现有 DTO，不自行推断新状态 |
| `components/FieldRow.vue` | 统一标签/值、敏感信息、等宽字段、复制和空值展示 | 不增加当前 API 没有的字段 |
| `DeviceCard.vue` / `DeviceOverviewTab.vue` | 借鉴“状态摘要 + 关键指标 + 分区详情”的层次 | DJOneHub 只有一个设备，改成单设备概览，不做卡片网格和设备列表 |
| `Sms.vue` | 借鉴左侧会话列表、右侧会话明细、搜索、空态和加载覆盖层 | 保留当前短信 API；不加入参考端的多设备选择、逐条删除和历史分页契约 |
| `DeviceEsimTab.vue` | 借鉴 Profile 卡片、状态标签、分组字段、危险操作确认和操作提示 | 只保留当前下载/启用/改名/删除和备注接口 |
| `DeviceAtTab.vue` | 借鉴终端式输入区、结果区和 capability 不可用说明 | 保留当前预设和解析结果；不新增超时、历史、调试收集等功能 |
| `EmptyState.vue` / `ErrorState.vue` / `ListSkeleton.vue` | 为每个页面建立一致的加载、失败、空数据和离线展示 | 错误文本继续走现有 i18n 和 APIError |
| `TrafficAnalysisPanel.vue` | 不采用，仅参考面板分组和刷新操作的视觉处理 | 当前端只展示实时速度和本次流量 |

参考实现依旧只作为本地设计和代码组织参考；新组件应按 DJOneHub 的数据模型重新实现，不把 `vohive-open/web` 的整套页面、服务、依赖和产品文案搬入 `web/`。

## 目标信息架构

保留当前导航分组，统一成参考端的“应用壳层 + 页面内容”结构：

```text
DJOneHub
├── 控制
│   ├── 概览
│   ├── 消息
│   ├── eSIM
│   ├── 网络
│   └── GPS
├── 语音
│   ├── 通话
│   └── VoWiFi
└── 工具
    ├── AT 调试
    └── 设置
```

每个页面统一包含：

1. `PageHeader`：页面标题、简短说明、刷新或当前页面已有的主要操作。
2. 页面级错误/离线提示：保留重试入口和 capability 原因。
3. 主要内容区：使用统一 `Card/Panel`、`FieldRow`、状态标签和操作栏。
4. 异步反馈：在原位置显示成功、失败、进行中和最终状态，不用空白或突然跳转代替反馈。

## 组件与文件拆分方案

当前 `App.vue` 同时承担导航、所有页面状态、请求、轮询、事件刷新和模板渲染。实施时按以下边界拆分，数据逻辑先保持不变：

```text
web/src/
├── App.vue                         # 只负责组装 AppShell 和当前视图
├── components/
│   ├── AppShell.vue                # 侧栏、移动端导航、主内容框架
│   ├── PageHeader.vue              # 页面标题和操作区
│   ├── StatusLight.vue             # 状态点/状态标签
│   ├── FieldRow.vue                # 标签和值的统一展示
│   ├── Panel.vue                   # 页面面板和标题栏
│   ├── EmptyState.vue              # 空数据/未连接/不支持
│   ├── ErrorState.vue              # API 错误和重试
│   ├── LoadingState.vue             # 初始加载骨架或遮罩
│   ├── OperationStatus.vue          # operation_id 进度和最终状态
│   ├── CapabilityNotice.vue         # capability 不足的明确说明
│   └── ...                          # 仅在至少两个页面复用时提取
├── views/
│   ├── OverviewView.vue
│   ├── CallsView.vue
│   ├── SmsView.vue
│   ├── EsimView.vue
│   ├── NetworkView.vue
│   ├── GPSView.vue
│   ├── RawATView.vue
│   ├── VowifiView.vue
│   └── SettingsView.vue
├── services/                        # 现有 API 和 AT 解析继续复用
├── stores/                          # 现有 device store 继续复用
└── style.css                        # 统一设计 token、响应式和状态样式
```

拆分时不为了“组件数量”而拆分：只有跨页面重复的布局和状态模式进入公共组件，页面特有的业务状态仍由对应 view 管理。

## 实施顺序

### 阶段 0：基线冻结

- [x] 完成当前 `web/` 与 `vohive-open/web/` 的结构和功能范围对比。
- [x] 记录当前已有页面、API 和明确排除项。
- [x] 记录现有 `npm run typecheck`、`npm run build` 基线结果。

### 阶段 1：应用壳层和基础组件

- [x] 从 `App.vue` 抽出 `AppShell`、`PageHeader`、`Panel`、`FieldRow`、`StatusLight`、`EmptyState`、`ErrorState` 和 `OperationStatus`。
- [x] 将侧栏改成固定宽度、清晰分组、选中态明确的单设备导航；保留移动端折叠和当前导航项。
- [x] 调整主内容最大宽度、页头间距、卡片边界、按钮层级和表单控件，使页面密度接近参考端。
- [x] 建立统一的成功、警告、错误、离线、加载和不可用样式，不改变现有文案和业务状态。

### 阶段 2：概览优先

- [x] 将概览改成“设备状态摘要 + 关键指标 + 无线/网络详情 + capability”布局。
- [x] 采用参考端的状态灯、字段行和分组面板表现连接、SIM、驻网、信号和 backend。
- [x] 无硬件启动时显示明确的单设备 offline/未连接状态，不能出现空白页面或假设备卡片。

### 阶段 3：核心业务页

- [x] 短信：采用稳定的双栏会话布局；窄屏改为上下/单栏流；保留搜索、会话、发送、验证码复制、刷新、清理和 operation 状态。
- [x] eSIM：采用 Profile 卡片和状态标签；把下载、Profile 设置、危险操作和备注分成清晰的弹窗/区块；保留敏感字段打码。
- [x] 网络：采用“当前网络摘要 + 流量指标 + 检查结果 + 模式/策略操作”布局；实时轮询进入明确的刷新状态，不加入历史图表。

### 阶段 4：其余页面和设置

- [x] 通话：用活动通话状态块和历史列表表达轮询、空态和拒接操作。
- [x] GPS：用状态摘要、定位字段和操作栏表达开关、刷新、最后定位和错误。
- [x] AT：用终端式输入/结果布局整理预设、命令、解析结果和原始响应；无 `raw_at` 时显示 capability 原因。
- [x] VoWiFi：用 readiness/status 区块、状态字段、操作栏和 operation 进度表达现有状态；不宣称未验证的数据面能力。
- [x] 设置：保留语言和敏感标识两个已有设置，用参考端的分区面板表达，不新增登录、主题或系统设置。

### 阶段 5：一致性和验证

- [ ] 所有页面检查桌面宽度、平板宽度和手机宽度，确保标题、按钮、长 ICCID/IMEI、错误信息和弹窗不溢出。
- [ ] 检查键盘焦点、`aria-label`、按钮禁用态、移动端导航关闭和弹窗关闭行为。
- [ ] 检查所有 capability 受限按钮：无能力时禁用或隐藏，并给出服务端原因；不按操作系统名称分支。
- [ ] 检查 WebSocket 事件、轮询、operation 终态和页面切换后的清理行为没有回归。
- [x] 运行 `npm run typecheck` 和 `npm run build`。
- [ ] 使用无硬件状态和至少一组模拟数据检查概览、短信、eSIM、网络、AT、VoWiFi 的加载/空/错/成功状态。
- [ ] 在桌面和移动视口留存截图，确认没有新增参考端范围外的导航、接口或功能。

本轮反馈验证：

- [x] 成功操作反馈统一使用 Ant Design Vue 右上角 Toast，持续约 3.5 秒后自动消失；页面内不再保留短信、eSIM、网络、VoWiFi、通知调试和设置成功提示。
- [x] 使用 Chrome DevTools 验证短信刷新成功 Toast 的右上角位置、显示内容和自动消失行为。

## 不变的行为契约

- 不修改 `/api/v1` 路由、请求方法、请求体、DTO 字段或 operation/event 契约。
- 继续使用 `web/src/services/api.ts`、`web/src/stores/device.ts`、`web/src/types.ts` 和现有 i18n；必要时只做拆分和类型收敛。
- 所有操作按钮继续由 capability 控制；不能把按钮可点击等同于浏览器或操作系统可用。
- 继续保持敏感信息默认打码，并尊重本地 `localStorage` 设置。
- 保留无硬件启动、设备断开、事件重同步、轮询刷新和异步 operation 的现有逻辑。
- 只修改前端重构所需的 `web/` 文件和本 task 文档；不覆盖工作区中其他未提交改动。

## 完成判定

任务完成必须同时满足：

1. 所有现有页面和操作都能从新的布局访问，且现有 API 行为不变。
2. 页面拥有统一的壳层、页头、面板、字段、状态、加载、空态、错误和异步进度表现。
3. 桌面端和移动端没有横向溢出、按钮遮挡、长标识撑破布局或弹窗超出视口。
4. capability 缺失、设备离线、API 失败和 operation 失败均有明确可理解的反馈。
5. `npm run typecheck` 和 `npm run build` 通过。
6. 代码审查确认没有引入代理、多设备、登录、通知、USSD、历史分析等范围外能力。

## 相关参考

- 当前前端：[web/](../web/)
- 参考实现：[vohive-open/web/](../vohive-open/web/)
- 当前范围审计：[CROSS_PLATFORM_FUSION_SCOPE_AUDIT.md](CROSS_PLATFORM_FUSION_SCOPE_AUDIT.md)
- Vue 管理端边界：[CROSS_PLATFORM_FUSION_VUE_MIGRATION.md](CROSS_PLATFORM_FUSION_VUE_MIGRATION.md)
- OpenSpec UI 约束：[vue-management-ui/spec.md](../openspec/changes/cross-platform-fusion/specs/vue-management-ui/spec.md)
