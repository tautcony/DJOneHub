# DJOneHub for macOS

DJOneHub 的 macOS 用户发行入口只有 DMG。DMG 包含一个带原生 UI 的 `DJOneHub.app`、安装脚本和卸载脚本；安装后由一个用户级 LaunchAgent 管理生命周期。

## 用户安装

1. 从 Releases 下载与架构匹配的 DMG。
2. 双击 DMG 内的 `安装 DJOneHub.command`。
3. 安装脚本将 App 复制到 `~/Library/Application Support/DJOneHub/DJOneHub.app`。
4. macOS 注册 `~/Library/LaunchAgents/com.jamie.djonehub.plist` 并自动启动服务。
5. 管理页面地址为 `http://127.0.0.1:7575`。

卸载时双击 DMG 内的 `卸载 DJOneHub.command`。它会停止 LaunchAgent、删除 App 和启动配置，但保留日志目录。

日志：

```text
~/Library/Logs/DJOneHub/launchd.log
```

预览包使用临时签名，首次运行可能需要在“系统设置 → 隐私与安全性”中允许打开。

## 构建发行包

要求：

- macOS 13 或更新版本
- Go 1.26.3 或兼容版本
- Node.js 和 npm
- Xcode Command Line Tools
- `pkg-config`
- 可访问 GitHub Release 的网络，用于下载并校验 libusb 1.0.30

Apple Silicon DMG：

```sh
./scripts/build-dmg.sh v0.1.5-preview
```

Intel + Apple Silicon 通用 DMG：

```sh
./scripts/build-dmg-universal.sh v0.1.5-preview
```

构建结果：

```text
dist/DJOneHub-macOS-arm64-<version>.dmg
dist/DJOneHub-macOS-arm64-<version>.dmg.sha256
dist/DJOneHub-macOS-universal-<version>.dmg
dist/DJOneHub-macOS-universal-<version>.dmg.sha256
```

`scripts/package-macos-arm64.sh` 和 `scripts/package-macos-universal.sh` 只负责生成 DMG 使用的 App staging 目录，不是用户安装入口。

## 本地开发

安装前端依赖：

```sh
npm --prefix web install
```

无硬件演示：

```sh
./scripts/dev.sh -demo
```

开发前端监听 `127.0.0.1:5176`，后端监听 `127.0.0.1:7576`。macOS 本地原生 UI 测试产物：

```sh
./scripts/build-macos.sh
```

## 平台边界

- macOS 适配器识别 DJI `2ca3:4006` 和 Quectel `2c7c:0125` USB 身份。
- 用户发行包只管理一个物理设备。
- 功能由设备、后端和平台能力快照决定；未验证的功能不会被前端执行。
- eSIM Profile 操作依赖实体 eUICC 卡片和模块固件。
