# DJOneHub

DJOneHub 是面向单个蜂窝模块的本地管理工具。它通过模块已有的 USB、串口、QMI 或 MBIM 接口，提供设备状态、短信、SIM/eSIM、网络诊断、原始 AT 指令和 VoWiFi 状态管理等能力。管理页面由 Vue 3 构建，后端由 Go 提供；默认只监听本机地址，数据和运行状态保存在本机。

项目当前主要围绕大疆第一代 4G 模块及其 Quectel USB 身份开发。它是非官方第三方软件，与 DJI、Quectel、运营商或 eSIM 服务商不存在隶属、授权或合作关系。

## 支持范围

### 硬件

macOS 适配器当前识别以下 USB 身份：

| 设备 | VID:PID | 说明 |
| --- | --- | --- |
| DJI 4G 模块 | `2ca3:4006` | 大疆原始 USB 组合模式 |
| Quectel 4G 模块 | `2c7c:0125` | 转换后的 Quectel USB 组合模式 |

程序只管理一个物理设备。设备重新枚举或切换 USB 模式后，运行时会根据物理位置、USB 身份及调制解调器标识关联回同一设备；多个模块同时连接不属于当前产品边界。

### 平台

| 平台 | 当前状态 | 说明 |
| --- | --- | --- |
| macOS 13+ Apple Silicon | 已提供发行包 | 当前硬件验证和主要用户流程集中在此平台 |
| macOS Intel | 构建脚本支持，实机验证有限 | 请以具体 Release 的说明为准 |
| Linux | 源码适配中 | 已有 USB/sysfs、网络和部分传输适配，不等同于完整发行支持 |
| Windows | 实验性适配 | 当前主要用于验证统一运行时和构建路径，硬件能力仍受适配器覆盖范围限制 |

后端会把实际可用能力发布给前端。某个平台或后端未实现的功能会显示为不支持，不应根据操作系统名称推断功能可用性。

### 功能

- 设备发现、连接生命周期、运营商、信号、注册状态和 SIM 状态
- SIM 卡档案：以 ICCID 为主键维护历史插卡记录（实体卡/eSIM 均自动建档），维护名称、电话号码与备注；插卡自动补录 IMSI/MSISDN 与首次/最近见到时间
- 短信读取、发送、长短信重组、验证码识别和模块短信存储清理
- 兼容实体 eUICC/eSIM 卡片的 Profile 读取、下载（支持二维码/激活码与确认码交互）、启用、停用、改名、删除和待处理通知管理；通知处置历史持久化在本地 SQLite（`djonehub.sqlite3`）
- USB 网络状态、接口地址、连通性、速度和当前会话流量统计
- 原始 AT 指令调试
- VoWiFi 状态查看，以及在平台和设备能力允许时执行启用、停用和重连
- 浅色、深色和跟随系统的管理页面外观

具体功能由设备、固件、SIM/eUICC、运营商网络和当前后端共同决定。eSIM Profile 写入、网络模式切换和原始 AT 指令都可能改变设备或卡片状态。

## 普通用户：安装发行包

普通用户应从项目的 Releases 页面下载发行包。GitHub 自动生成的 `Source code (zip)` 和 `Source code (tar.gz)` 只是源码快照，不能替代已打包的应用。

当前 macOS 只保留 DMG 作为用户发行格式。DMG 内含一个可拖入“应用程序”目录的 `DJOneHub.app`。

下载 Release 提供的 DMG 后，可在终端执行以下命令校验文件：

```sh
shasum -a 256 DJOneHub-macOS-*.dmg
```

将结果与 Release 提供的校验值比较。

### DMG 安装

打开 DMG，将 `DJOneHub.app` 拖入“应用程序”目录后启动。管理页面地址为 `http://127.0.0.1:7575`。

登录启动在应用“设置 → 登录时启动”中管理。应用会按当前用户创建或移除：

```text
~/Library/LaunchAgents/com.jamie.djonehub.plist
```

移除应用前，请先在设置中关闭登录启动；日志和本地数据不会随应用移除。

### macOS 安全提示

预览发行包可能使用临时签名，未经过 Apple Developer ID 公证。首次运行被阻止时，请在“系统设置 → 隐私与安全性”中确认允许打开。

如果 DMG 中的 App 仍提示文件已损坏，只对来自可信 Release、且已核对 SHA-256 的发行包执行：

```sh
xattr -dr com.apple.quarantine ./DJOneHub.app
```

## 使用边界

### USB 与网络模式

模块的 USB 组合模式切换会触发重新枚举，页面短暂显示设备断开属于预期现象。切换过程中不要拔出模块，也不要在 eSIM 写入或其他关键操作期间关闭程序。

USB 网络是否能正常上网还取决于 SIM 套餐、网络注册、APN、macOS 网络服务和运营商限制。页面中的流量只统计当前 DJOneHub 进程运行期间的本地观测值，不是运营商账单。

### 短信

短信收发受网络注册、短信中心、套餐、漫游和模块固件影响。发送国际短信时使用完整国际号码，例如：

```text
+86138XXXXXXXX
+447700900XXX
```

“清理模块旧短信”只处理模块内部存储，不等于删除运营商侧或其他设备中的短信。

### eSIM Profile

此功能管理插在模块实体卡槽中的兼容 eUICC/eSIM 卡片，不管理 Mac 内置 eSIM。Profile 下载、启用、改名和删除都会写入实体卡片；删除通常不可撤销。不同卡片即使遵循相同规范，也可能存在扩展能力差异，使用前应确认卡片和账户支持相关操作。

### 原始 AT

AT 调试可以改变网络注册、PDP、USB 模式、短信存储和 SIM 状态。不了解作用的指令不要执行，不要直接使用来源不明的刷机或写入命令。

## 开发者：从源码运行

### 环境要求

- Go `1.26.3` 或兼容版本，版本约束见 `go.mod`
- Node.js 和 npm，用于构建 Vue 前端
- macOS 本地构建还需要 Xcode Command Line Tools 和可用的 Apple 编译工具链
- 构建 macOS 发行包时，需要从 GitHub 下载并校验 libusb 1.0.30 源码

安装前端依赖：

```sh
npm --prefix web install
```

### 本地开发

无硬件时，一键启动 Go 后端和 Vite 前端：

```sh
./scripts/dev.sh -demo
```

开发地址：

```text
前端：http://127.0.0.1:5176
后端：http://127.0.0.1:7576
```

Vite 会把 `/api` 和 WebSocket 请求代理到后端。连接真实设备时去掉 `-demo`：

```sh
./scripts/dev.sh
```

也可以分别启动：

```sh
./scripts/dev-backend.sh -demo
./scripts/dev-web.sh
```

后端命令行参数：

```text
-listen 127.0.0.1:7576   HTTP 监听地址
-web-dir web/dist         Vue 构建产物目录
-demo                     不访问真实硬件
```

### 测试与构建

运行 Go 测试和前端校验：

```sh
go test ./...
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run build
```

构建 macOS 本地测试 App：

```sh
./scripts/build-macos-dev.sh
```

构建 Apple Silicon DMG：

```sh
./scripts/build-macos.sh arm64 v0.1.0-preview
```

构建 Intel + Apple Silicon 通用 DMG：

```sh
./scripts/build-macos.sh universal v0.1.0-preview
```

libusb 源码包会缓存到 `dist/cache/libusb/`。需要强制重新下载时，在命令末尾添加 `--redownload`。

发行构建通过 `libusb` 构建标签启用真实 USB AT 实现，并将脚本编译的 libusb 1.0.30 一起打包。普通 `go test ./...` 不启用该标签，使用 stub，不要求本机安装 libusb；本地 macOS 测试 App `scripts/build-macos-dev.sh` 会启用该标签并需要本机 libusb 头文件和库。

构建跨平台基础二进制：

```sh
./scripts/build-platforms.sh
```

产物位于 `dist/`；正式用户发行物是其中的 `.dmg` 文件及其 SHA-256 校验文件。`dist/release/` 下的 App staging 目录是 DMG 构建中间产物，不是独立安装格式。

## 代码结构

```text
cmd/djonehub/          统一服务入口，托管 API 和 Vue 静态资源
internal/domain/       设备、能力、错误和业务领域模型
internal/application/  设备、短信、eSIM、网络、AT、VoWiFi 等用例服务
internal/backend/      AT、QMI、MBIM 后端及统一能力契约
internal/runtime/      单设备生命周期、事件和资源锁
internal/platform/     Linux、macOS、Windows 平台适配器
internal/api/http/     本地 HTTP/WebSocket API
internal/storage/      SQLite 和本地配置存储
pkg/mbim/              MBIM 协议实现
pkg/smscodec/          SMS PDU 编解码和长短信重组
web/                   Vue 3 + TypeScript + Vite 管理前端
packaging/             macOS 发行包第三方声明
scripts/               开发、测试和打包脚本
third_party/           构建实际使用的本地第三方依赖
```

API 使用 `/api/v1` 版本路径，异步操作通过 operation ID 和 WebSocket 事件更新。前端根据服务端能力快照展示功能，因此新增平台适配器时必须同时验证设备发现、传输、网络和能力声明，而不能只让代码通过编译。

更详细的目录说明见 [SOURCE_STRUCTURE.md](SOURCE_STRUCTURE.md)，平台说明见 [MACOS.md](MACOS.md)，接口约定见 [docs/native-bridge-contract.md](docs/native-bridge-contract.md) 及 `openspec/specs/`。

## 日志、本地数据与隐私

macOS 默认路径：

```text
~/Library/Logs/DJOneHub/djonehub.log
~/Library/Application Support/DJOneHub/
```

短信缓存、Profile 备注、通知偏好和运行状态属于本地数据。提交 Issue、日志或截图前，应隐藏手机号、EID、ICCID、IMSI、短信正文和验证码等敏感信息。

如需手动删除日志，请先确认不再需要：

```sh
rm -rf "$HOME/Library/Logs/DJOneHub"
```

## 许可证与第三方声明

本项目遵循 [PolyForm Noncommercial License 1.0.0](LICENSE)。仓库包含基于 VoHive 演进的代码，必须保留上游声明：

```text
Required Notice: Copyright iniwex5 (https://github.com/iniwex5/vohive)
```

第三方依赖和随发行包提供的 libusb 许可证见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) 及各 `third_party` 目录。商业使用、再分发和衍生版本请先确认对应许可证和权利人的授权要求。
