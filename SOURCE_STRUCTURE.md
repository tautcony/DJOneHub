# DJOneHub 源码结构

这份文档描述当前源码边界和 macOS 发行流程。Vue 前端位于 `web/`，统一 Go 服务入口位于 `cmd/djonehub/`。

## 目录

```text
cmd/djonehub/          统一服务入口，托管 API 和 Vue 静态资源
internal/domain/       设备、能力、错误和领域模型
internal/application/  设备、短信、eSIM、网络、AT、VoWiFi 用例
internal/backend/      AT、QMI、MBIM 后端和能力契约
internal/runtime/      单设备生命周期、事件和资源锁
internal/platform/     Linux、macOS、Windows 平台适配器
internal/api/http/     本地 HTTP/WebSocket API
internal/storage/      SQLite 和本地配置存储
pkg/mbim/              MBIM 协议实现
pkg/smscodec/          SMS PDU 编解码和长短信重组
web/                   Vue 3 + TypeScript + Vite 管理前端
macos/DJOneHubNotifier/ Swift 原生 UI 静态库，链接进 macOS Go 主程序
scripts/               开发、测试和 DMG 构建脚本
packaging/             macOS 发行包第三方声明
third_party/           构建实际使用的本地第三方依赖
```

## 构建边界

所有平台使用 `cmd/djonehub`。共享业务代码通过 platform、transport 和 backend 接口访问操作系统和设备；macOS 原生 UI 通过 cgo 链接 `macos/DJOneHubNotifier` 静态库。

macOS 用户发行流程只有一条：

```text
scripts/build-macos.sh <arm64|universal> <version>
        |
        v
DJOneHub.app + DMG + SHA-256
        |
        v
拖入“应用程序”目录 -> 应用设置管理 LaunchAgent
```

正式发布使用 `scripts/build-macos.sh`，本地开发测试使用 `scripts/build-macos-dev.sh`。

## 验证

```sh
go test -mod=mod ./...
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run build
```

macOS DMG 构建脚本会校验 libusb 源码 SHA-256、构建 Vue 页面、构建 Swift 静态库、组装 App bundle、执行 ad-hoc 签名、生成 DMG 并写出 DMG SHA-256 文件。

当前 Go module 路径仍为 `github.com/iniwex5/vohive`，用于保持现有共享包导入路径和上游来源关系。
