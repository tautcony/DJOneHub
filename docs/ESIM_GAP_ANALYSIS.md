# eSIM（eUICC）功能差距分析报告

> 日期：2026-08-07
> 范围：DJOneHub 当前 eSIM/eUICC 实现 vs 业界主流工具（Android LPA、iOS、SimpleESIM、GSMA SGP.22/SGP.32 等）
> 结论摘要：核心 SGP.22 下载/启用/删除/改名链路已完整落地；**Profile 停用（Disable）、eUICC 重置、SM-DS 发现、通知管理 UI、确认码交互、QMI/MBIM eUICC 传输启用**等业界标配能力尚未实现或未接线。
>
> 状态更新（2026-08-07，openspec 变更 `complete-esim-management`）：**P0/P1 五项差距（Profile 停用、下载阶段进度、确认码交互、通知管理、二维码扫描）已全部接线落地**，下文 §2 中对应项标记 ✅ 已实现；P2/P3 项仍为缺口。

---

## 1. 当前实现盘点（已实现）

### 1.1 核心 SGP.22 流程（`internal/esim/manager.go`，基于 vendored `third_party/euicc-go/lpa`）

| 功能 | 实现位置 | 说明 |
| --- | --- | --- |
| EID 读取（ES10c GetEID） | manager.go:1142/1192/1221 | 多 ISD-R AID 扫描 |
| eUICC 信息（ES10b EUICCInfo1/2 + ES10a 配置地址） | euicc_info.go:19-51 | 含 DefaultSMDP+/RootSMDS 地址读取 |
| Profile 列表（ES10c GetProfilesInfo） | manager.go:405-416/1836-1841 | ICCID/State/Nickname/SPN/Class 过滤 |
| Profile 启用/切换（EnableProfile, refresh=true） | manager.go:2164-2323 | CatBusy 重试×3、切换屏障、切换后置钩子 |
| Profile 删除 + 删除通知上报 | manager.go:3424-3558 | 删除后向 SM-DP+ 发通知 |
| Profile 改名（SetNickname） | manager.go:3372-3420 | |
| Profile 下载（完整 SGP.22 + ES9p HTTP 流程） | manager.go:3142-3301 | auth-client/auth-server/BPP install；matchingID + confirmationCode + IMEI；**写卡前空闲 NVRAM 预检（<80KB 中止防变砖）**；安装失败恢复 |
| 通知处理（ES10b，隐式） | manager.go:2836-2960/3052/3083 | 下载/删除路径内自动清理 enable/disable 通知 |
| eUICC 信息富化 | euicc_info.go:53-102 | FreeNvram、固件、SAS、厂商、CI 证书 |
| eSTK.me 私有产品信息 | manager.go:1322-1383 | 私有 APDU |
| 9eSIM SKU 推断 | manager.go:66-109 | 由 EID/固件推断 |
| 多厂商 ISD-R AID 扫描 | manager.go:32-46/1101-1189 | eSTK.me / GSMA / eSIM.me / 5ber / XeSIM / LinksField / GlocalMe |
| 错误映射 | download_error.go:62-134、errors.go:8-83 | BPP 错误（ICCID 已存在、无内存、PPR 拒绝、中断等） |
| PKI 查询 | internal/esim/pki/ | 仅查找（CI/AC 认证机构、厂商），**无证书链校验** |

### 1.2 传输通道

- **AT（生产可用）**：`AT+CCHO / AT+CGLA / AT+CCHC` 逻辑通道 APDU（at_channel.go:31-63），macOS 走 USB AT，Linux/Windows 走串口 AT（at_factory.go:72-77）
- **QMI（仅测试，生产未接线）**：QMI UIM OpenLogicalChannel/SendAPDU（qmi_uim_transport.go），QMIBackend 未构建 ESIMPort
- **MBIM（仅测试，生产未接线）**：MBIM UICC Low Level Access（mbim_apdu_transport.go），MBIMBackend 未构建 ESIMPort

### 1.3 前端与 API（`web/src/views/EsimView.vue`、`internal/api/http/server.go`）

- Profile 卡片：状态标签、掩码 ICCID、SPN、Profile Class、AID、本地备注
- 操作：**下载 / 启用 / 删除 / 改名（设置）**，无停用
- 下载对话框：激活码、确认码、matchingID 均为**手动文本输入**，无二维码扫描
- API：`GET /api/v1/esim`、`POST .../actions/{download,enable,rename,delete}`、`GET /health`、`GET|PUT /notes`

### 1.4 规范承诺（openspec/specs/）

所有 openspec 规范承诺均已兑现；差距均属"静默缺失"，非规范违约。

---

## 2. 未实现 / 未接线（差距清单）

> 下述差距均为核实过的代码事实；"库支持但未接线" 指 vendored `euicc-go` 已暴露接口、但仓库内无调用点。

### 2.1 Profile 停用（Disable）— ✅ 已实现（2026-08-07）

- ~~`Manager.DisableProfile` 存在（manager.go:2327-2435），但全仓库无调用者~~ → 已接线：`ESIMPort.Disable`（service_ports.go）、应用服务 `Disable`、`POST /api/v1/esim/actions/disable`、前端"停用"按钮（enabled 状态显示，含确认弹窗）
- 业界标配：Android/iOS/SimpleESIM 均提供停用（停用后可释放已启用名额，是双卡切换的基础操作）

### 2.2 eUICC 重置（MemoryReset）— 库已支持，未接线

- vendored 库暴露 `MemoryReset`（lpa/es10c.go:109），仓库内零调用
- 业界标配：Android 开发者选项/恢复出厂时提供"重置 eUICC"，可清除所有 Profile

### 2.3 SM-DS 发现（ES11 Discovery）— 库已支持，未接线

- vendored 库暴露 `Discovery`（lpa/es11.go:12），仓库内零调用
- RootSMDSAddress/DefaultSMDP+ 只读展示，从未实际连接 SM-DS
- 影响：无法支持运营商"空中开通/后台推送"类 Profile（业界主流 LPA 均实现 SM-DS 轮询）

### 2.4 通知（Notifications）管理 — ✅ 已实现（2026-08-07）

- `Manager.ListNotifications`（manager.go:3052）、`RetryNotification`（:3083）原仅包内使用 → 已接线：新增 `Manager.RemoveNotification`（:3083 同区域）、`GET /api/v1/esim/notifications`、`POST .../{seq}/process`、`DELETE .../{seq}`，前端通知面板（列表 + 重发/删除，内联结果）
- 下载/删除路径外的失败通知（如 SM-DP+ 侧失败回执）现可在面板中重发或清理
- **通知历史持久化（2026-08-07 增量）**：eUICC 通知处置轨迹落库 SQLite（`esim_notification_history` 表，schema v4，去重键 `(sequence_number, iccid, event)`，状态机 pending→processed/failed/removed）；应用服务以卡片快照 diff 驱动状态迁移（快照内 → pending，消失 → processed）；`GET /api/v1/esim/notifications/history` + 前端"通知历史"区展示（含已被自动清理的记录，可回溯此前 13–17 这类事件）

### 2.5 下载确认码交互 — ✅ 已实现（2026-08-07）

- 库的 `OnConfirm` / `OnEnterConfirmationCode` 回调（download.go:107-108）已接线：manager 层新增 `DownloadInteraction`，经 operation 事件（`esim.confirmation_code_request`）+ `POST /api/v1/esim/operations/{operation_id}/confirmation-code` 回复端点完成交互；用户拒绝或 5 分钟超时按结构化取消处理（不再直接中止报错）
- 业界主流：iOS/Android 在下载中提示输入确认码；eSIM 套餐普遍使用确认码防误装

### 2.6 下载进度不透明 — ✅ 已实现（2026-08-07）

- at_port.go 原传入 `nil` progressFn → 已透传 manager 的 `DownloadProgressEvent{Step, Pct, Msg}`（manager.go:2476-2483，preflight/auth_client/auth_server/install/notify 各阶段），operation 进度事件携带阶段文案
- 顺带修复：完整 `LPA:1$smdp$matchingID` 激活码（二维码内容）此前无法直接粘贴使用，现由应用服务 `resolveActivationCode` 解析拆分

### 2.7 二维码 / OTP 输入 — ✅ 已实现（2026-08-07，图片/粘贴；OTP 仍为手动输入）

- 下载对话框新增"扫描二维码"：图片文件选择 + 剪贴板粘贴 → jsqr 解码 → 填入激活码输入框；解码失败有引导文案
- 摄像头实时扫码未做（桌面端权限/焦点复杂度），留作后续增强；OTP 与确认码共用输入框，属业界一致做法

### 2.8 QMI / MBIM eUICC 传输 — 生产死代码

- `NewQMIUIMTransportWithOptions` / `NewMBIMAPDUTransport` / `NewQMIChannel` 仅测试引用
- QMI/MBIM 后端未构建 ESIMPort → **eSIM 只能走 AT 通道**
- 影响：Windows（无 USB AT 桥）/ Linux QMI 场景下 eSIM 不可用；Quectel 模块 QMI 原生 eSIM（QMI_UIM_APDU）是业界主流方案

### 2.9 其他细节

- `esim.changed` 事件有消费者（runtime.go:248）无生产者
- 前端引用 `card_type:'physical_sim'`（EsimView.vue:74），后端从不产出该值
- PKI 仅查询不校验：下载流程未做 GSMA 证书链/签名验证（依赖 HTTP + 库内部校验，需确认 euicc-go 行为）
- 库暴露但未用：`SetDefaultDPAddress`（es10a.go:30）、`EUICCChallenge`（es10b.go:11）、`ListProfile`（es10c.go:21）

---

## 3. 与业界主流工具对比

### 3.1 参照系说明

业界"消费级 eSIM 管理器"的标准能力面 = GSMA SGP.22 的 ES10a/b/c + ES9+（ES10c 本地 Profile 管理 + ES10b 下载/安全通道 + ES9+ SM-DP+ 接口 + ES11 SM-DS 发现）。与本项目最接近的同类工具是 **lpac + EasyLPAC**（桌面端开源 eSIM 管理器，SGP.22 v2.2.2，PC/SC/AT/MBIM/QMI 后端）和 **Quectel 模块自带 AT+QESIM**（同硬件生态的 LPA）。Android LPA（AOSP）、iOS、Windows 内置为平台级参照。SGP.32（IoT）对消费工具不在范围内（ES10c 明确被 SGP.32 排除），本项目无需考虑。

### 3.2 功能对比矩阵

| 能力项 | **DJOneHub（现状）** | lpac/EasyLPAC | Android LPA | OpenEUICC | Windows 内置 | Quectel AT+QESIM | iOS |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Profile 列表（状态/昵称/SPN/Class/图标） | ✅（无图标） | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ |
| 下载（激活码+确认码+matchingID/IMEI） | ✅ 手动文本 | ✅ 含确认码、IMEI、预览 | ✅ | ✅ | ✅ QR/手动 | ✅ | ✅ QR/链接 |
| 二维码扫描输入 | ❌ | ⚠️（文本为主） | ✅ | ✅ | ✅ | ⚠️ | ✅ |
| 下载中确认码交互（OnConfirm） | ❌ 缺失即中止 | ✅ 交互式预览 | ✅ LUI 可解析错误 | ✅ | ✅ | ⚠️ | ✅ |
| 下载阶段进度 | ❌ 仅 5%/100% | ✅ 阶段日志 | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Profile 停用（Disable）** | ❌ 库支持未接线 | ✅ 含 refreshFlag | ✅ | ✅ | ✅ | ✅ | ✅ |
| 启用/切换（Enable, refresh） | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| 删除（Delete+通知上报） | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| 改名（SetNickname） | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| eUICC 信息（EID/EUICCInfo2/地址） | ✅ EID+Info2+地址 | ✅ | ✅ | ✅ | ⚠️ | ⚠️ EID | ⚠️ EID |
| **通知管理（列出/重发/删除）** | ❌ 无 API/UI | ✅ auto-process | ✅ | ✅ | ✅ | ⚠️ | ⚠️ 静默 |
| **SM-DS 发现（ES11）** | ❌ 库支持未接线 | ✅ | ✅ | ✅ | ✅ | ⚠️ | ⚠️ 内部 |
| **eUICC 重置（MemoryReset）** | ❌ 库支持未接线 | ✅ chip purge | ✅ | ✅ | ✅ Wipe | ❌ | ❌ |
| 多 Profile/PPR 感知 | ⚠️ 仅显示 Class | ⚠️ | ✅ PPR 码 | ✅ | ⚠️ | ⚠️ | ⚠️ |
| 刷新处理（refreshFlag/REFRESH） | ✅ 启用/停用时 | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ 自动 |
| 错误处理/用户指引 | ✅ 错误码映射 | ⚠️ JSON 码 | ✅ SMDX 细码+LUI | ✅ | ⚠️ | ⚠️ | ⚠️ |
| GSMA PKI/证书校验 | ⚠️ 仅查询不校验 | ✅ | ✅ | ✅ | ✅ | ✅（经 LPA） | ✅ |
| 传输通道 | AT ✅ / QMI、MBIM 仅测试 | PC/SC、AT、MBIM、QMI | 芯片驱动 | 芯片驱动 | MBIM eSIM | QMI+AT | 芯片驱动 |

### 3.3 结论要点

1. **已超越/持平部分**：下载完整流程（含防变砖 NVRAM 预检、matchingID、确认码参数、安装失败恢复）、启用/删除/改名、eUICC 信息读取、多厂商 ISD-R AID 兼容（eSTK.me/5ber/eSIM.me/XeSIM 等）——这些与 lpac、Android LPA 相当。
2. **明显落后部分**：停用（Disable）、通知管理、SM-DS 发现、eUICC 重置、确认码交互、下载进度、二维码——全部是业界主流 LPA 的标配，本项目"库已支持但未接线"或完全缺失。
3. **调研修正**（与常见认知相反，值得记录）：
   - SGP.22 中 Profile 状态只有 `disabled(0)/enabled(1)` 两种，**不存在 "enabled-and-disabled" 状态**（本实现 Profile.State 三值显示是兼容的）；
   - SGP.22 无 `SetFallbackAddress` 函数，只有 `GetEuiccConfiguredAddresses`/`SetDefaultDpAddress`；
   - "Reset Mode 1/2/3" 属于 3GPP REFRESH（TS 31.111）的 Reset Mode 枚举，不是 SGP.22 定义；
   - QMI 侧不存在公开的 "GetProvisionedProfiles" 消息，Profile 枚举走 QMI_UIM_APDU 传输 ES10c 命令；
   - "SimpleESIM"、"esim.id" 未能在开源社区核实存在，同类开源项目实际是 lpac/EasyLPAC、OpenEUICC/EasyEUICC、lpa-gtk、LPAdesktop。

---

## 4. 差距优先级与实施建议

> 排序原则：业界标配程度 × 实现成本（大部分差距底层库已支持，仅需接线）× 产品影响面。

### P0 — 业界标配缺失，接线成本低（✅ 已于 2026-08-07 随 complete-esim-management 变更落地）

| # | 差距 | 现状 | 实施要点 |
| --- | --- | --- | --- |
| 1 | **Profile 停用（Disable）全链路** | `Manager.DisableProfile`（manager.go:2327）已实现、零调用 | ESIMPort 加 `Disable`（service_ports.go:18-25）→ 应用服务 `Disable`（仿 Enable，带 ResourceSIM 锁）→ `POST /api/v1/esim/actions/disable` → 前端停用按钮（EsimView.vue 现有"启用"仅当 `state === 'disabled'` 显示，可对称加"停用"仅当 `state === 'enabled'`）。停用是双 Profile 切换的基础，lpac/Android/iOS/Windows 均有 |
| 2 | **下载阶段进度上抛** | at_port.go:115 传 nil progressFn；应用服务只报 5%/100% | at_port.go 接入 manager 的 `DownloadProgressFn`（auth_client → auth_server → install → notify 各阶段），映射到 operation progress 推送前端；EsimView.vue 已绑定 OperationStatusView |

### P1 — 业界标配缺失，中等成本（✅ 已于 2026-08-07 随 complete-esim-management 变更落地）

| # | 差距 | 实施要点 |
| --- | --- | --- |
| 3 | **下载确认码交互** | 使用 euicc-go 的 `OnConfirm`/`OnEnterConfirmationCode`（download.go:107-108）；下载开始后若服务端要求确认码，通过 operation 事件向 UI 发"需确认码"提示并暂挂，用户输入后继续。当前行为（缺失即中止）与 Android LUI、lpac 交互式预览差距明显 |
| 4 | **通知管理（API + UI）** | `Manager.ListNotifications`（manager.go:3052）、`RetryNotification`（:3083）已有实现，补 `GET /api/v1/esim/notifications`、`POST .../process`、`DELETE .../{seq}`；前端设置页加通知列表；参考 lpac `notification process [-a] [-r]` |
| 5 | **二维码扫描** | 激活码解析已由 euicc-go `ActivationCode.UnmarshalText` 完成（download.go:47），缺的只是扫码入口；前端集成二维码扫码库（如 zxing-js），把 `LPA:1$...` 文本填入下载对话框；同时保留手动粘贴（本就支持） |

### P2 — 能力补齐，成本较高（建议按需做）

| # | 差距 | 实施要点 |
| --- | --- | --- |
| 6 | **QMI/MBIM eUICC 传输启用** | `QMIUIMTransport`（qmi_uim_transport.go:28）、`MBIMAPDUTransport`（mbim_apdu_transport.go:23）均为可用实现但只有测试引用；在 QMIBackend/MBIMBackend 上构建 ESIMPort（参照 AT 的注入模式）。收益：Windows（无 USB AT 桥）与 Linux QMI 场景可用 eSIM，这是 Quectel 模块业界主流路径（Qualcomm UIM 的 QMI_UIM_APDU / Windows MBIM_CID_MS_UICC_APDU） |
| 7 | **eUICC 重置（MemoryReset）** | 调 euicc-go `MemoryReset`（es10c.go:109）；需强确认 UI（lpac `chip purge` 要求输入 "yes"）；注意与 `esim.updated` 事件和前端列表刷新联动 |
| 8 | **SM-DS 发现（ES11）** | 调 `Discovery`（es11.go:12）；RootSMDSAddress 已从 `EUICCConfiguredAddresses` 读出（euicc_info.go:42），可做"检查运营商推送"按钮 + 事件列表。收益：支持运营商空中开通类流程 |

### P3 — 体验增强（可长期迭代）

| # | 差距 | 实施要点 |
| --- | --- | --- |
| 9 | Profile 图标与 PPR 显示 | ProfileInfo 含 iconType/icon（JPG/PNG 64×64）与 PPR 位（do-not-disable/do-not-delete/delete-after-disabling）；euicc-go ListProfile 可带 icon tag，前端渲染 |
| 10 | 错误码细粒度展示 | 借鉴 Android `SMDX_SUBJECT/REASON_CODE` 拆分展示；把 SGP.22 `subjectCode/reasonCode` 透出到前端错误文案 |
| 11 | 安全增强 | pki/ 目前仅查询（ci.json/accredited.json 查找），确认 euicc-go 下载路径的证书链/签名校验行为；缺口则补 GSMA CI 根证书链校验（参考 GSMA eSIM Certificates 与 Osmocom eUICC manual） |
| 12 | `esim.changed` 事件生产者 | runtime.go:248 有消费者无生产者；后端在 Profile 变更后发布该事件，实现跨端自动刷新 |

---

## 5. 参考

### 本地代码（本文引用）

- `internal/esim/manager.go`、`at_port.go`、`at_channel.go`、`euicc_info.go`、`qmi_uim_transport.go`、`mbim_apdu_transport.go`、`pki/`
- `internal/backend/service_ports.go`（ESIMPort）、`at_backend.go`、`qmi_backend.go`
- `internal/application/esim/service.go`
- `internal/api/http/server.go`
- `web/src/views/EsimView.vue`、`web/src/stores/esim.ts`
- `third_party/euicc-go/lpa/`（vendored LPA 库，`go.mod:9` replace 指向）

### 业界工具与规范

- GSMA SGP.22 v2.6（消费级 RSP 规范）：https://www.gsma.com/solutions-and-impact/technologies/esim/wp-content/uploads/2024/09/SGP.22-v2.6.pdf
- GSMA SGP.32 v1.0.1（IoT eSIM）：https://www.gsma.com/solutions-and-impact/technologies/esim/wp-content/uploads/2023/05/SGP.32-1.0.1.pdf
- AOSP EuiccManager/EuiccService/EuiccCardManager（android15-release）：https://android.googlesource.com/platform/frameworks/base/+/android15-release/telephony/java/android/telephony/euicc/ 与 https://source.android.com/docs/core/connect/esim-overview
- lpac（桌面 LPA CLI，SGP.22 v2.2.2）：https://github.com/estkme-group/lpac
- EasyLPAC（lpac GUI）：https://github.com/creamlike1024/EasyLPAC
- OpenEUICC / EasyEUICC：https://github.com/AKoskovich/OpenEUICC
- Apple Support — Set up eSIM on iPhone：https://support.apple.com/en-ie/118669 ；Dual SIM：https://support.apple.com/en-mide/109317
- Microsoft Learn — MB eSIM operations（MBIM_CID_MS_UICC_APDU 等）：https://learn.microsoft.com/en-us/windows-hardware/drivers/network/mb-esim-operations
- Quectel eSIM LPA Application Note（AT+QESIM）：https://forums.quectel.com/uploads/short-url/hQDPVOqtTwgWqjvyyrWEtS1y1Q4.pdf
- libqmi UIM service：https://gitlab.freedesktop.org/mobile-broadband/libqmi/-/raw/main/src/libqmi-glib/qmi-enums-uim.h
- GSMA eSIM 根 CI 证书：https://www.gsma.com/solutions-and-impact/technologies/esim/gsma-root-ci/ ；Osmocom eUICC manual：https://euicc-manual.osmocom.org/docs/pki/ci/
- eSIM.me（物理 eUICC 卡 + App）：https://play.google.com/store/apps/details?id=esim.me
- Truphone LPAdesktop：https://github.com/Truphone/LPAdesktop

### 调研修正记录

- "SimpleESIM"、"esim.id" 未能在开源社区/应用商店核实存在，报告不再引用（经 GitHub API 与多轮搜索确认，2026-08-07）。
- SGP.22 Profile 状态仅有 `disabled(0)/enabled(1)`，无 "enabled-and-disabled"；"Reset Mode 1/2/3" 出自 3GPP TS 31.111，非 SGP.22。
