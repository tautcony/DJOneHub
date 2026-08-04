# DJOneHubNotifier（原生 UI 模块）

DJOneHub 的 macOS 原生 UI 层，作为**静态库**链接进 Go 主进程（`internal/platform/darwin/native` 通过 cgo 调用），不再作为独立进程运行：

- UserNotifications 系统通知、可选的 AppKit 自绘提示面板、菜单栏 4G 图标和通知动作。
- 不访问 USB、不访问 HTTP、不轮询、不维护去重状态 —— 事件与命令全部通过原生桥接（`docs/native-bridge-contract.md`）与 Go 主进程交换。
- 非 macOS 平台不参与编译。

## 构建

```bash
./build-app.sh
```

产出静态库 `libDJOneHubNotifier.a`（Go 构建链接用）并执行 CLI 自检。Go 侧本地测试入口见 `scripts/build-macos-dev.sh`，发行入口见 `scripts/build-macos.sh`；二者都会把最新静态库链接到带 `Info.plist` 的 `DJOneHub.app`，避免裸可执行文件无法获得 macOS 通知 bundle 身份。

通知显示方式在 Web 设置中按来电、未接来电、短信和设备离线分别选择“系统通知”或“自绘面板”。macOS 不向普通 App 提供 FaceTime/电话式 CallKit 通话界面，自绘面板只是可选的 AppKit 视觉方案。

## 开发工具（CLI）

`DJOneHubNotifierCLI` 仅供桥接契约自检，不进入发行包：

```bash
swift run -c release DJOneHubNotifierCLI --self-test
```

## 测试

```bash
swift test
```

覆盖事件 DTO 解码（含小数秒时间戳）、文本格式化与命令编码。
